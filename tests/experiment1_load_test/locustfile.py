"""
Experiment 1: API Load Testing
Progressively increase concurrent users to measure throughput, latency, and error rates.

Usage:
    locust -f tests/experiment1_load_test/locustfile.py --host http://<ALB_DNS>
    # Then open http://localhost:8089 to configure and start the test
"""

import io
import random

from locust import HttpUser, task, between


class PrintQueueUser(HttpUser):
    wait_time = between(1, 3)

    def on_start(self):
        """Create a few initial jobs for this user."""
        self.user_id = f"loadtest-user-{random.randint(1, 10000)}"
        self.held_jobs = []
        self.all_jobs = []

        # Upload 3 seed jobs
        for _ in range(3):
            job = self._upload_job()
            if job:
                self.held_jobs.append(job)
                self.all_jobs.append(job)

    def _upload_job(self):
        fake_content = f"Test document content {random.randint(1, 1_000_000)}".encode()
        files = {"file": ("test-doc.pdf", io.BytesIO(fake_content), "application/pdf")}
        data = {"userId": self.user_id}

        with self.client.post("/jobs", files=files, data=data, catch_response=True) as resp:
            if resp.status_code == 201:
                return resp.json()
            else:
                resp.failure(f"Upload failed: {resp.status_code}")
                return None

    @task(3)
    def upload_job(self):
        """Upload a new print job — heaviest operation."""
        job = self._upload_job()
        if job:
            self.held_jobs.append(job)
            self.all_jobs.append(job)

    @task(2)
    def poll_status(self):
        """Poll a random job's status."""
        if not self.all_jobs:
            return
        job = random.choice(self.all_jobs)
        self.client.get(f"/jobs/{job['jobId']}", name="/jobs/[id]")

    @task(1)
    def release_job(self):
        """Release a held job to a random printer."""
        if not self.held_jobs:
            return
        job = self.held_jobs.pop(0)
        printer = random.choice(["printer-1", "printer-2", "printer-3"])
        with self.client.post(
            f"/jobs/{job['jobId']}/release",
            json={"printerName": printer},
            name="/jobs/[id]/release",
            catch_response=True,
        ) as resp:
            if resp.status_code not in (200, 409):
                resp.failure(f"Release failed: {resp.status_code}")

    @task(1)
    def list_jobs(self):
        """List jobs for this user."""
        self.client.get(f"/jobs?userId={self.user_id}", name="/jobs?userId=[id]")
