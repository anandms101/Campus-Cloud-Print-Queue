"""Unit tests for the printer worker processor.

Uses moto to mock DynamoDB and S3 so tests run without real AWS resources.
"""

import importlib
import pytest
import boto3

TABLE_NAME = "test-print-jobs"
BUCKET_NAME = "test-print-docs"
REGION = "us-west-2"


def _make_sqs_message(job_id, s3_key, receive_count="1"):
    from app.processor import PrintJob
    return PrintJob(
        job_id=job_id,
        s3_key=s3_key,
        receive_count=int(receive_count),
        receipt_handle="test-receipt",
    )


@pytest.fixture
def worker_env(full_worker_env, monkeypatch):
    """Reload worker modules so they pick up mocked AWS clients."""
    import app.config as cfg
    importlib.reload(cfg)
    import app.services.dynamodb as ddb
    importlib.reload(ddb)
    import app.services.s3 as s3mod
    importlib.reload(s3mod)
    import app.processor as proc
    importlib.reload(proc)

    return full_worker_env


def _table():
    return boto3.resource("dynamodb", region_name=REGION).Table(TABLE_NAME)


def _create_released_job(job_id, s3_key="uploads/test/doc.pdf"):
    _table().put_item(Item={
        "jobId": job_id, "userId": "testuser", "fileName": "doc.pdf",
        "s3Key": s3_key, "status": "RELEASED", "printerName": "test-printer",
        "createdAt": "2025-01-01T00:00:00", "updatedAt": "2025-01-01T00:00:00",
        "expiresAt": 9999999999, "version": 2,
    })


def _create_held_job(job_id, s3_key="uploads/test/doc.pdf"):
    _table().put_item(Item={
        "jobId": job_id, "userId": "testuser", "fileName": "doc.pdf",
        "s3Key": s3_key, "status": "HELD",
        "createdAt": "2025-01-01T00:00:00", "updatedAt": "2025-01-01T00:00:00",
        "expiresAt": 9999999999, "version": 1,
    })


def _create_processing_job(job_id, s3_key="uploads/test/doc.pdf"):
    _table().put_item(Item={
        "jobId": job_id, "userId": "testuser", "fileName": "doc.pdf",
        "s3Key": s3_key, "status": "PROCESSING", "printerName": "test-printer",
        "createdAt": "2025-01-01T00:00:00", "updatedAt": "2025-01-01T00:00:00",
        "expiresAt": 9999999999, "version": 2,
    })


def _create_done_job(job_id, s3_key="uploads/test/doc.pdf"):
    _table().put_item(Item={
        "jobId": job_id, "userId": "testuser", "fileName": "doc.pdf",
        "s3Key": s3_key, "status": "DONE", "printerName": "test-printer",
        "createdAt": "2025-01-01T00:00:00", "updatedAt": "2025-01-01T00:00:00",
        "expiresAt": 9999999999, "version": 3,
    })


def _put_s3_object(s3_key, content=b"test PDF content"):
    boto3.client("s3", region_name=REGION).put_object(
        Bucket=BUCKET_NAME, Key=s3_key, Body=content
    )


def _get_status(job_id):
    resp = _table().get_item(Key={"jobId": job_id})
    return resp["Item"]["status"]


class TestProcessMessage:
    def test_normal_processing_flow(self, worker_env):
        from app.processor import process_message

        job_id = "job-001"
        s3_key = "uploads/job-001/doc.pdf"
        _create_released_job(job_id, s3_key)
        _put_s3_object(s3_key)

        result = process_message(_make_sqs_message(job_id, s3_key))

        assert result is True
        assert _get_status(job_id) == "DONE"

    def test_already_done_is_idempotent(self, worker_env):
        from app.processor import process_message

        job_id = "job-done"
        s3_key = "uploads/job-done/doc.pdf"
        _create_done_job(job_id, s3_key)

        result = process_message(_make_sqs_message(job_id, s3_key))

        assert result is True
        assert _get_status(job_id) == "DONE"

    def test_s3_download_failure_marks_failed(self, worker_env):
        from app.processor import process_message

        job_id = "job-missing-s3"
        s3_key = "uploads/job-missing-s3/missing.pdf"
        _create_released_job(job_id, s3_key)

        result = process_message(_make_sqs_message(job_id, s3_key))

        assert result is True
        assert _get_status(job_id) == "FAILED"

    def test_held_job_skipped(self, worker_env):
        from app.processor import process_message

        job_id = "job-held"
        s3_key = "uploads/job-held/doc.pdf"
        _create_held_job(job_id, s3_key)

        result = process_message(_make_sqs_message(job_id, s3_key))

        assert result is True
        assert _get_status(job_id) == "HELD"

    def test_redelivery_of_processing_job(self, worker_env):
        from app.processor import process_message

        job_id = "job-redelivery"
        s3_key = "uploads/job-redelivery/doc.pdf"
        _create_processing_job(job_id, s3_key)
        _put_s3_object(s3_key)

        result = process_message(_make_sqs_message(job_id, s3_key, receive_count="2"))

        assert result is True
        assert _get_status(job_id) == "DONE"


class TestWorkerDynamoDB:
    def test_mark_processing_succeeds(self, worker_env):
        from app.services.dynamodb import mark_processing

        job_id = "job-mp"
        _create_released_job(job_id)

        assert mark_processing(job_id) is True
        assert _get_status(job_id) == "PROCESSING"

    def test_mark_processing_fails_on_held(self, worker_env):
        from app.services.dynamodb import mark_processing

        job_id = "job-mp-held"
        _create_held_job(job_id)

        assert mark_processing(job_id) is False
        assert _get_status(job_id) == "HELD"

    def test_mark_done_succeeds(self, worker_env):
        from app.services.dynamodb import mark_done

        job_id = "job-md"
        _create_processing_job(job_id)

        assert mark_done(job_id) is True
        assert _get_status(job_id) == "DONE"

    def test_mark_done_fails_on_wrong_status(self, worker_env):
        from app.services.dynamodb import mark_done

        job_id = "job-md-wrong"
        _create_held_job(job_id)

        assert mark_done(job_id) is False
        assert _get_status(job_id) == "HELD"

    def test_mark_failed(self, worker_env):
        from app.services.dynamodb import mark_failed

        job_id = "job-mf"
        _create_processing_job(job_id)

        mark_failed(job_id)
        assert _get_status(job_id) == "FAILED"
