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

PRINTERS = ["printer-1", "printer-2", "printer-3"]


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
        printer = random.choice(PRINTERS)
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

    @task(1)
    def cancel_job(self):
        """Cancel a held job and remove it from tracking lists."""
        if not self.held_jobs:
            return
        job = self.held_jobs.pop(0)
        with self.client.delete(
            f"/jobs/{job['jobId']}",
            name="/jobs/[id] (DELETE)",
            catch_response=True,
        ) as resp:
            if resp.status_code == 200:
                # Remove from all_jobs so we don't poll a cancelled job.
                self.all_jobs = [j for j in self.all_jobs if j["jobId"] != job["jobId"]]
            elif resp.status_code == 409:
                # Already released — acceptable race condition.
                resp.success()
            else:
                resp.failure(f"Cancel failed: {resp.status_code}")

    @task(1)
    def list_jobs_by_status(self):
        """List jobs for this user filtered by a specific status."""
        status = random.choice(["HELD", "RELEASED", "CANCELLED"])
        self.client.get(
            f"/jobs?userId={self.user_id}&status={status}",
            name="/jobs?userId=[id]&status=[status]",
        )

    @task(1)
    def health_check(self):
        """Verify the health endpoint stays responsive under load."""
        with self.client.get("/health", catch_response=True) as resp:
            if resp.status_code != 200:
                resp.failure(f"Health check failed: {resp.status_code}")

    @task(1)
    def poll_nonexistent_job(self):
        """GET a job ID that doesn't exist — exercises the 404 path."""
        fake_id = f"00000000-0000-0000-0000-{random.randint(0, 999999999999):012d}"
        with self.client.get(
            f"/jobs/{fake_id}",
            name="/jobs/[nonexistent-id]",
            catch_response=True,
        ) as resp:
            if resp.status_code == 404:
                resp.success()
            else:
                resp.failure(f"Expected 404, got {resp.status_code}")

    @task(1)
    def release_with_invalid_printer(self):
        """POST release with a bad printerName — exercises the 400 validation path."""
        if not self.all_jobs:
            return
        job = random.choice(self.all_jobs)
        with self.client.post(
            f"/jobs/{job['jobId']}/release",
            json={"printerName": "printer-invalid"},
            name="/jobs/[id]/release (bad printer)",
            catch_response=True,
        ) as resp:
            if resp.status_code == 400:
                resp.success()
            else:
                resp.failure(f"Expected 400, got {resp.status_code}")
