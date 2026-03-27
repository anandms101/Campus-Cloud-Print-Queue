"""
Experiment 3: Printer Saturation and Queueing Behavior
Flood a single printer's queue with M jobs and observe backpressure and degradation.

Usage:
    python tests/experiment3_saturation/saturation_test.py --url http://<ALB_DNS> --jobs 50 --printer printer-1
"""

import argparse
import asyncio
import io
import json
import time
import aiohttp


async def create_and_release_job(session: aiohttp.ClientSession, base_url: str, printer: str, idx: int) -> dict:
    # Create job
    data = aiohttp.FormData()
    data.add_field("file", io.BytesIO(f"Saturation test doc #{idx}".encode()), filename=f"test-{idx}.pdf", content_type="application/pdf")
    data.add_field("userId", "saturation-tester")

    async with session.post(f"{base_url}/jobs", data=data) as resp:
        if resp.status != 201:
            return {"error": f"Create failed: {resp.status}", "idx": idx}
        job = await resp.json()

    # Release to target printer
    async with session.post(
        f"{base_url}/jobs/{job['jobId']}/release",
        json={"printerName": printer},
    ) as resp:
        return {
            "jobId": job["jobId"],
            "idx": idx,
            "release_status": resp.status,
            "released_at": time.time(),
        }


async def poll_until_done(session: aiohttp.ClientSession, base_url: str, job_ids: list[str], timeout: int = 600) -> list[dict]:
    """Poll all jobs until they're DONE or timeout."""
    results = {jid: {"jobId": jid, "done_at": None, "final_status": None} for jid in job_ids}
    start = time.time()

    while time.time() - start < timeout:
        pending = [jid for jid, r in results.items() if r["done_at"] is None]
        if not pending:
            break

        # Poll a batch
        for jid in pending[:20]:  # Poll 20 at a time
            try:
                async with session.get(f"{base_url}/jobs/{jid}") as resp:
                    if resp.status == 200:
                        job = await resp.json()
                        if job["status"] in ("DONE", "FAILED", "CANCELLED"):
                            results[jid]["done_at"] = time.time()
                            results[jid]["final_status"] = job["status"]
            except Exception:
                pass

        remaining = len([r for r in results.values() if r["done_at"] is None])
        print(f"  Polling... {len(job_ids) - remaining}/{len(job_ids)} complete", end="\r")
        await asyncio.sleep(5)

    print()
    return list(results.values())


async def main():
    parser = argparse.ArgumentParser(description="Printer saturation test")
    parser.add_argument("--url", required=True, help="Base URL of the API")
    parser.add_argument("--jobs", type=int, default=50, help="Number of jobs to release")
    parser.add_argument("--printer", default="printer-1", help="Target printer")
    args = parser.parse_args()

    print(f"\n{'='*70}")
    print(f"Experiment 3: Printer Saturation Test")
    print(f"  Jobs: {args.jobs}, Printer: {args.printer}")
    print(f"{'='*70}")

    async with aiohttp.ClientSession() as session:
        # Phase 1: Create and release all jobs
        print(f"\nReleasing {args.jobs} jobs to {args.printer}...")
        start_release = time.time()

        tasks = [
            create_and_release_job(session, args.url, args.printer, i)
            for i in range(args.jobs)
        ]
        release_results = await asyncio.gather(*tasks)
        release_time = time.time() - start_release

        successful = [r for r in release_results if "jobId" in r]
        failed = [r for r in release_results if "error" in r]

        print(f"  Released: {len(successful)}, Failed: {len(failed)}")
        print(f"  Release time: {release_time:.1f}s")

        # Phase 2: Poll until all done
        print(f"\nWaiting for jobs to complete...")
        job_ids = [r["jobId"] for r in successful]
        completion_results = await poll_until_done(session, args.url, job_ids)

        done = [r for r in completion_results if r["final_status"] == "DONE"]
        not_done = [r for r in completion_results if r["done_at"] is None]

        print(f"\nResults:")
        print(f"  Completed: {len(done)}/{len(successful)}")
        print(f"  Timed out: {len(not_done)}")

        # Calculate latencies
        release_times = {r["jobId"]: r["released_at"] for r in successful}
        latencies = []
        for r in done:
            if r["jobId"] in release_times:
                latency = r["done_at"] - release_times[r["jobId"]]
                latencies.append(latency)

        if latencies:
            latencies.sort()
            print(f"\n  End-to-end latency:")
            print(f"    Min:  {min(latencies):.1f}s")
            print(f"    p50:  {latencies[len(latencies)//2]:.1f}s")
            print(f"    p95:  {latencies[int(len(latencies)*0.95)]:.1f}s")
            print(f"    Max:  {max(latencies):.1f}s")
            print(f"    Mean: {sum(latencies)/len(latencies):.1f}s")

        # Save results
        output = {
            "config": {"jobs": args.jobs, "printer": args.printer},
            "release_time_s": release_time,
            "completed": len(done),
            "timed_out": len(not_done),
            "latencies_s": latencies,
        }
        with open("tests/experiment3_saturation/results.json", "w") as f:
            json.dump(output, f, indent=2)
        print(f"\nResults written to tests/experiment3_saturation/results.json")


if __name__ == "__main__":
    asyncio.run(main())
