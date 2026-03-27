"""
Experiment 2: DynamoDB Contention and Conditional Writes
Fire N parallel release requests against the same jobId to validate exactly-one-wins semantics.

Usage:
    python tests/experiment2_contention/contention_test.py --url http://<ALB_DNS> --concurrency 2,5,10,20,50
"""

import argparse
import asyncio
import io
import json
import time
import aiohttp


async def create_job(session: aiohttp.ClientSession, base_url: str) -> dict:
    data = aiohttp.FormData()
    data.add_field("file", io.BytesIO(b"contention test doc"), filename="test.pdf", content_type="application/pdf")
    data.add_field("userId", "contention-tester")

    async with session.post(f"{base_url}/jobs", data=data) as resp:
        assert resp.status == 201, f"Job creation failed: {resp.status}"
        return await resp.json()


async def release_job(session: aiohttp.ClientSession, base_url: str, job_id: str, printer: str) -> dict:
    start = time.time()
    async with session.post(
        f"{base_url}/jobs/{job_id}/release",
        json={"printerName": printer},
    ) as resp:
        elapsed = (time.time() - start) * 1000
        return {
            "status_code": resp.status,
            "latency_ms": elapsed,
            "body": await resp.json() if resp.content_type == "application/json" else {},
        }


async def run_contention_test(base_url: str, n: int) -> dict:
    async with aiohttp.ClientSession() as session:
        # Create a fresh job
        job = await create_job(session, base_url)
        job_id = job["jobId"]

        # Fire N concurrent release requests
        tasks = [
            release_job(session, base_url, job_id, "printer-1")
            for _ in range(n)
        ]
        results = await asyncio.gather(*tasks)

        successes = [r for r in results if r["status_code"] == 200]
        conflicts = [r for r in results if r["status_code"] == 409]
        errors = [r for r in results if r["status_code"] not in (200, 409)]

        latencies = [r["latency_ms"] for r in results]

        return {
            "concurrency": n,
            "total_requests": n,
            "successes": len(successes),
            "conflicts": len(conflicts),
            "errors": len(errors),
            "avg_latency_ms": sum(latencies) / len(latencies),
            "max_latency_ms": max(latencies),
            "min_latency_ms": min(latencies),
            "exactly_one_success": len(successes) == 1,
        }


async def main():
    parser = argparse.ArgumentParser(description="DynamoDB contention test")
    parser.add_argument("--url", required=True, help="Base URL of the API (e.g., http://ALB_DNS)")
    parser.add_argument("--concurrency", default="2,5,10,20,50", help="Comma-separated concurrency levels")
    args = parser.parse_args()

    levels = [int(x) for x in args.concurrency.split(",")]

    print(f"\n{'='*70}")
    print(f"Experiment 2: DynamoDB Contention Test")
    print(f"{'='*70}")

    all_results = []
    for n in levels:
        print(f"\n--- Concurrency level: {n} ---")
        result = await run_contention_test(args.url, n)
        all_results.append(result)

        print(f"  Successes:  {result['successes']} (expected: 1)")
        print(f"  Conflicts:  {result['conflicts']} (expected: {n - 1})")
        print(f"  Errors:     {result['errors']}")
        print(f"  Avg latency: {result['avg_latency_ms']:.1f} ms")
        print(f"  Exactly one winner: {'PASS' if result['exactly_one_success'] else 'FAIL'}")

    # Summary
    print(f"\n{'='*70}")
    print("Summary")
    print(f"{'='*70}")
    print(f"{'N':>5} | {'Success':>7} | {'Conflict':>8} | {'Error':>5} | {'Avg(ms)':>8} | {'Pass':>4}")
    print("-" * 55)
    for r in all_results:
        print(f"{r['concurrency']:>5} | {r['successes']:>7} | {r['conflicts']:>8} | {r['errors']:>5} | {r['avg_latency_ms']:>8.1f} | {'Y' if r['exactly_one_success'] else 'N':>4}")

    # Write raw results
    with open("tests/experiment2_contention/results.json", "w") as f:
        json.dump(all_results, f, indent=2)
    print("\nResults written to tests/experiment2_contention/results.json")


if __name__ == "__main__":
    asyncio.run(main())
