#!/usr/bin/env python3
"""Seed the system with test jobs for experiments."""

import argparse
import io
import random
import requests


def main():
    parser = argparse.ArgumentParser(description="Seed test jobs")
    parser.add_argument("--url", required=True, help="API base URL")
    parser.add_argument("--count", type=int, default=10, help="Number of jobs to create")
    parser.add_argument("--release", action="store_true", help="Also release jobs to random printers")
    args = parser.parse_args()

    printers = ["printer-1", "printer-2", "printer-3"]
    jobs = []

    for i in range(args.count):
        files = {"file": (f"test-doc-{i}.pdf", io.BytesIO(f"Test document {i}".encode()), "application/pdf")}
        data = {"userId": f"seed-user-{random.randint(1, 5)}"}

        resp = requests.post(f"{args.url}/jobs", files=files, data=data)
        if resp.status_code == 201:
            job = resp.json()
            jobs.append(job)
            print(f"Created job {job['jobId'][:8]}... (status: {job['status']})")
        else:
            print(f"Failed to create job {i}: {resp.status_code}")

    if args.release:
        for job in jobs:
            printer = random.choice(printers)
            resp = requests.post(
                f"{args.url}/jobs/{job['jobId']}/release",
                json={"printerName": printer},
            )
            status = resp.json().get("status", "error") if resp.status_code == 200 else f"err:{resp.status_code}"
            print(f"Released {job['jobId'][:8]}... to {printer} (status: {status})")

    print(f"\nDone. Created {len(jobs)} jobs.")


if __name__ == "__main__":
    main()
