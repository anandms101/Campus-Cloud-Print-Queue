"""Unit tests for the FastAPI print job API.

Uses moto to mock DynamoDB, S3, and SQS so tests run without real AWS resources.
"""

import io
import json
import importlib
import pytest


@pytest.fixture
def api_client(full_api_env, monkeypatch):
    """Create a FastAPI TestClient with mocked AWS backends."""
    import app.config as config_mod
    importlib.reload(config_mod)

    import app.services.dynamodb as dynamo_mod
    importlib.reload(dynamo_mod)
    import app.services.s3 as s3_mod
    importlib.reload(s3_mod)
    import app.services.sqs as sqs_mod
    importlib.reload(sqs_mod)

    import app.routes.jobs as jobs_mod
    importlib.reload(jobs_mod)
    import app.main as main_mod
    importlib.reload(main_mod)

    from fastapi.testclient import TestClient
    yield TestClient(main_mod.app)


def _upload_job(client, user_id="testuser", filename="test.pdf", content=b"PDF content"):
    return client.post(
        "/jobs",
        data={"userId": user_id},
        files={"file": (filename, io.BytesIO(content), "application/pdf")},
    )


class TestHealthEndpoint:
    def test_health_returns_200(self, api_client):
        resp = api_client.get("/health")
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "healthy"
        assert "timestamp" in data


class TestCreateJob:
    def test_create_job_success(self, api_client):
        resp = _upload_job(api_client)
        assert resp.status_code == 201
        data = resp.json()
        assert data["status"] == "HELD"
        assert data["userId"] == "testuser"
        assert data["fileName"] == "test.pdf"
        assert "jobId" in data

    def test_create_job_missing_user_id(self, api_client):
        resp = api_client.post(
            "/jobs",
            files={"file": ("test.pdf", io.BytesIO(b"data"), "application/pdf")},
        )
        assert resp.status_code == 422

    def test_create_job_missing_file(self, api_client):
        resp = api_client.post("/jobs", data={"userId": "testuser"})
        assert resp.status_code == 422

    def test_create_job_file_too_large(self, api_client):
        large_content = b"x" * (51 * 1024 * 1024)
        resp = _upload_job(api_client, content=large_content)
        assert resp.status_code == 413

    def test_create_job_sanitizes_filename(self, api_client):
        resp = _upload_job(api_client, filename="../../../etc/passwd")
        assert resp.status_code == 201
        data = resp.json()
        assert data["fileName"] == "passwd"
        assert ".." not in data["fileName"]


class TestGetJob:
    def test_get_job_success(self, api_client):
        create_resp = _upload_job(api_client)
        job_id = create_resp.json()["jobId"]

        resp = api_client.get(f"/jobs/{job_id}")
        assert resp.status_code == 200
        assert resp.json()["jobId"] == job_id

    def test_get_job_not_found(self, api_client):
        resp = api_client.get("/jobs/nonexistent-id")
        assert resp.status_code == 404


class TestListJobs:
    def test_list_jobs_by_user(self, api_client):
        _upload_job(api_client, user_id="alice")
        _upload_job(api_client, user_id="alice")
        _upload_job(api_client, user_id="bob")

        resp = api_client.get("/jobs?userId=alice")
        assert resp.status_code == 200
        jobs = resp.json()
        assert len(jobs) == 2
        assert all(j["userId"] == "alice" for j in jobs)

    def test_list_jobs_requires_user_id(self, api_client):
        resp = api_client.get("/jobs")
        assert resp.status_code == 422

    def test_list_jobs_with_status_filter(self, api_client):
        _upload_job(api_client, user_id="carol")
        _upload_job(api_client, user_id="carol")

        resp = api_client.get("/jobs?userId=carol&status=HELD")
        assert resp.status_code == 200
        jobs = resp.json()
        assert len(jobs) == 2
        assert all(j["status"] == "HELD" for j in jobs)

        resp = api_client.get("/jobs?userId=carol&status=DONE")
        assert resp.status_code == 200
        assert len(resp.json()) == 0


class TestReleaseJob:
    def test_release_job_success(self, api_client):
        create_resp = _upload_job(api_client)
        job_id = create_resp.json()["jobId"]

        resp = api_client.post(
            f"/jobs/{job_id}/release",
            json={"printerName": "printer-1"},
        )
        assert resp.status_code == 200
        assert resp.json()["status"] == "RELEASED"

    def test_release_job_invalid_printer(self, api_client):
        create_resp = _upload_job(api_client)
        job_id = create_resp.json()["jobId"]

        resp = api_client.post(
            f"/jobs/{job_id}/release",
            json={"printerName": "nonexistent-printer"},
        )
        assert resp.status_code == 400

    def test_release_job_missing_printer_name(self, api_client):
        create_resp = _upload_job(api_client)
        job_id = create_resp.json()["jobId"]

        resp = api_client.post(f"/jobs/{job_id}/release", json={})
        assert resp.status_code == 422

    def test_release_job_not_found(self, api_client):
        resp = api_client.post(
            "/jobs/nonexistent-id/release",
            json={"printerName": "printer-1"},
        )
        assert resp.status_code == 404

    def test_release_already_released_returns_409(self, api_client):
        create_resp = _upload_job(api_client)
        job_id = create_resp.json()["jobId"]

        api_client.post(f"/jobs/{job_id}/release", json={"printerName": "printer-1"})

        resp = api_client.post(
            f"/jobs/{job_id}/release",
            json={"printerName": "printer-2"},
        )
        assert resp.status_code == 409


class TestCancelJob:
    def test_cancel_job_success(self, api_client):
        create_resp = _upload_job(api_client)
        job_id = create_resp.json()["jobId"]

        resp = api_client.delete(f"/jobs/{job_id}")
        assert resp.status_code == 200
        assert resp.json()["status"] == "CANCELLED"

    def test_cancel_job_not_found(self, api_client):
        resp = api_client.delete("/jobs/nonexistent-id")
        assert resp.status_code == 404

    def test_cancel_already_released_returns_409(self, api_client):
        create_resp = _upload_job(api_client)
        job_id = create_resp.json()["jobId"]

        api_client.post(f"/jobs/{job_id}/release", json={"printerName": "printer-1"})

        resp = api_client.delete(f"/jobs/{job_id}")
        assert resp.status_code == 409
