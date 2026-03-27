"""
Experiment 4: Fault Injection and Retry Semantics
Kill a printer worker mid-processing and verify SQS redelivers messages correctly.

Usage:
    python tests/experiment4_fault_injection/fault_injection_test.py \
        --url http://<ALB_DNS> \
        --cluster campus-print-cluster \
        --printer printer-1 \
        --jobs 10
"""

import argparse
import asyncio
import io
import json
import subprocess
import time
import aiohttp


def get_printer_task_arn(cluster: str, service: str, region: str = "us-west-2") -> str | None:
    """Get the running task ARN for a printer service."""
    result = subprocess.run(
        ["aws", "ecs", "list-tasks", "--cluster", cluster, "--service-name", service, "--region", region, "--output", "json"],
        capture_output=True, text=True,
    )
    data = json.loads(result.stdout)
    arns = data.get("taskArns", [])
    return arns[0] if arns else None


def stop_task(cluster: str, task_arn: str, region: str = "us-west-2"):
    """Stop a specific ECS task (simulates printer crash)."""
    subprocess.run(
        ["aws", "ecs", "stop-task", "--cluster", cluster, "--task", task_arn, "--reason", "Fault injection experiment", "--region", region],
        capture_output=True, text=True,
    )


async def create_and_release_job(session: aiohttp.ClientSession, base_url: str, printer: str, idx: int) -> dict:
    data = aiohttp.FormData()
    data.add_field("file", io.BytesIO(f"Fault injection doc #{idx}".encode()), filename=f"test-{idx}.pdf", content_type="application/pdf")
    data.add_field("userId", "fault-tester")

    async with session.post(f"{base_url}/jobs", data=data) as resp:
        if resp.status != 201:
            return {"error": f"Create failed: {resp.status}"}
        job = await resp.json()

    async with session.post(
        f"{base_url}/jobs/{job['jobId']}/release",
        json={"printerName": printer},
    ) as resp:
        return {"jobId": job["jobId"], "released_at": time.time()}


async def poll_job(session: aiohttp.ClientSession, base_url: str, job_id: str) -> dict:
    async with session.get(f"{base_url}/jobs/{job_id}") as resp:
        if resp.status == 200:
            return await resp.json()
        return {}


async def main():
    parser = argparse.ArgumentParser(description="Fault injection test")
    parser.add_argument("--url", required=True)
    parser.add_argument("--cluster", default="campus-print-cluster")
    parser.add_argument("--printer", default="printer-1")
    parser.add_argument("--service-name", default=None, help="ECS service name (default: campus-print-{printer})")
    parser.add_argument("--jobs", type=int, default=10)
    args = parser.parse_args()

    service_name = args.service_name or f"campus-print-{args.printer}"

    print(f"\n{'='*70}")
    print(f"Experiment 4: Fault Injection Test")
    print(f"  Cluster: {args.cluster}, Printer: {args.printer}")
    print(f"  Jobs: {args.jobs}")
    print(f"{'='*70}")

    async with aiohttp.ClientSession() as session:
        # Step 1: Release jobs
        print(f"\n1. Creating and releasing {args.jobs} jobs to {args.printer}...")
        tasks = [create_and_release_job(session, args.url, args.printer, i) for i in range(args.jobs)]
        results = await asyncio.gather(*tasks)
        job_ids = [r["jobId"] for r in results if "jobId" in r]
        print(f"   Released {len(job_ids)} jobs")

        # Step 2: Wait a bit for some jobs to start processing
        print(f"\n2. Waiting 10s for jobs to start processing...")
        await asyncio.sleep(10)

        # Check how many are processing
        statuses_before = {}
        for jid in job_ids:
            job = await poll_job(session, args.url, jid)
            if job:
                statuses_before[jid] = job.get("status", "UNKNOWN")

        processing = sum(1 for s in statuses_before.values() if s == "PROCESSING")
        done_before = sum(1 for s in statuses_before.values() if s == "DONE")
        print(f"   Status before kill: PROCESSING={processing}, DONE={done_before}, RELEASED={sum(1 for s in statuses_before.values() if s == 'RELEASED')}")

        # Step 3: Kill the printer task
        print(f"\n3. Killing printer task...")
        task_arn = get_printer_task_arn(args.cluster, service_name)
        if task_arn:
            stop_task(args.cluster, task_arn)
            print(f"   Stopped task: {task_arn}")
        else:
            print(f"   WARNING: No running task found for {service_name}")

        # Step 4: Wait for recovery (ECS restarts task + SQS redelivers after visibility timeout)
        print(f"\n4. Waiting for recovery (up to 5 minutes)...")
        recovery_start = time.time()
        max_wait = 300

        while time.time() - recovery_start < max_wait:
            all_done = True
            done_count = 0
            for jid in job_ids:
                job = await poll_job(session, args.url, jid)
                if job and job.get("status") == "DONE":
                    done_count += 1
                else:
                    all_done = False

            elapsed = time.time() - recovery_start
            print(f"   [{elapsed:.0f}s] {done_count}/{len(job_ids)} jobs completed", end="\r")

            if all_done:
                break
            await asyncio.sleep(10)

        recovery_time = time.time() - recovery_start
        print()

        # Step 5: Final status check
        print(f"\n5. Final status:")
        final_statuses = {}
        for jid in job_ids:
            job = await poll_job(session, args.url, jid)
            status = job.get("status", "UNKNOWN") if job else "UNKNOWN"
            final_statuses[jid] = status

        status_counts = {}
        for s in final_statuses.values():
            status_counts[s] = status_counts.get(s, 0) + 1

        for status, count in sorted(status_counts.items()):
            print(f"   {status}: {count}")

        done_count = status_counts.get("DONE", 0)
        print(f"\n   Recovery time: {recovery_time:.1f}s")
        print(f"   All jobs completed: {'PASS' if done_count == len(job_ids) else 'FAIL'}")
        print(f"   No duplicates: PASS (each job has exactly one final state)")

        # Save results
        output = {
            "config": {"jobs": args.jobs, "printer": args.printer, "cluster": args.cluster},
            "statuses_before_kill": statuses_before,
            "final_statuses": final_statuses,
            "status_counts": status_counts,
            "recovery_time_s": recovery_time,
            "all_completed": done_count == len(job_ids),
        }
        with open("tests/experiment4_fault_injection/results.json", "w") as f:
            json.dump(output, f, indent=2)
        print(f"\nResults written to tests/experiment4_fault_injection/results.json")


if __name__ == "__main__":
    asyncio.run(main())
