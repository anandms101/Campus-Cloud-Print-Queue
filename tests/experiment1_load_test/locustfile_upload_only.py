"""
Dedicated upload load test for POST /jobs.

Usage:
    locust -f tests/experiment1_load_test/locustfile_upload_only.py --host http://<ALB_DNS>
    # Then open http://localhost:8089 and start the test with many users.
"""

import os
import random

from locust import HttpUser, task, between, constant
import requests


class UploadOnlyUser(HttpUser):
    wait_time = constant(0)

    def on_start(self):
        self.user_id = f"loadtest-user-{random.randint(1, 1000000)}"

    @task
    def upload_job(self):
        root_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
        book_path = os.path.join(root_dir, "public", "book1.pdf")
        data = {"userId": self.user_id}

        if os.path.exists(book_path):
            with open(book_path, "rb") as f:
                files = {"file": ("book1.pdf", f, "application/pdf")}
                self._post_job(files, data)
        else:
            fake_content = f"Load test document content {random.randint(1, 1_000_000)}".encode()
            files = {"file": ("test-doc.pdf", fake_content, "application/pdf")}
            self._post_job(files, data)

    def _post_job(self, files, data):
        try:
            with self.client.post(
                "/jobs",
                files=files,
                data=data,
                catch_response=True,
                timeout=300,
                name="POST /jobs",
            ) as resp:
                if resp.status_code == 201:
                    resp.success()
                else:
                    resp.failure(f"Upload failed: {resp.status_code} - {resp.text}")
        except requests.RequestException as exc:
            raise
