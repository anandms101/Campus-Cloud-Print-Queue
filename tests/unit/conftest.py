"""Shared fixtures for unit tests using moto to mock AWS services."""

import os
import sys
import json
import pytest
import boto3
from moto import mock_aws

os.environ.setdefault("AWS_DEFAULT_REGION", "us-west-2")
os.environ.setdefault("AWS_ACCESS_KEY_ID", "testing")
os.environ.setdefault("AWS_SECRET_ACCESS_KEY", "testing")
os.environ.setdefault("AWS_SECURITY_TOKEN", "testing")
os.environ.setdefault("AWS_SESSION_TOKEN", "testing")

TABLE_NAME = "test-print-jobs"
BUCKET_NAME = "test-print-docs"
REGION = "us-west-2"

_REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
WORKER_SRC = os.path.join(_REPO_ROOT, "services", "printer-worker")


@pytest.fixture
def worker_path():
    """Add the worker source to sys.path."""
    if WORKER_SRC not in sys.path:
        sys.path.insert(0, WORKER_SRC)
    yield
    _purge_app_modules()


def _purge_app_modules():
    """Remove cached 'app' and 'app.*' modules so the next fixture gets a clean slate."""
    to_remove = [key for key in sys.modules if key == "app" or key.startswith("app.")]
    for key in to_remove:
        del sys.modules[key]


def _create_dynamodb_table(client):
    client.create_table(
        TableName=TABLE_NAME,
        KeySchema=[{"AttributeName": "jobId", "KeyType": "HASH"}],
        AttributeDefinitions=[
            {"AttributeName": "jobId", "AttributeType": "S"},
            {"AttributeName": "userId", "AttributeType": "S"},
            {"AttributeName": "createdAt", "AttributeType": "S"},
        ],
        GlobalSecondaryIndexes=[
            {
                "IndexName": "userId-createdAt-index",
                "KeySchema": [
                    {"AttributeName": "userId", "KeyType": "HASH"},
                    {"AttributeName": "createdAt", "KeyType": "RANGE"},
                ],
                "Projection": {"ProjectionType": "ALL"},
            }
        ],
        BillingMode="PAY_PER_REQUEST",
    )


@pytest.fixture
def full_worker_env(worker_path, monkeypatch):
    """Full mock AWS environment with worker source on path."""
    monkeypatch.setenv("DYNAMODB_TABLE", TABLE_NAME)
    monkeypatch.setenv("S3_BUCKET", BUCKET_NAME)
    monkeypatch.setenv("AWS_DEFAULT_REGION", REGION)
    monkeypatch.setenv("PRINTER_NAME", "test-printer")

    with mock_aws():
        dynamodb = boto3.client("dynamodb", region_name=REGION)
        _create_dynamodb_table(dynamodb)

        s3 = boto3.client("s3", region_name=REGION)
        s3.create_bucket(
            Bucket=BUCKET_NAME,
            CreateBucketConfiguration={"LocationConstraint": REGION},
        )

        sqs = boto3.client("sqs", region_name=REGION)
        queue_urls = {}
        for name in ["printer-1", "printer-2", "printer-3"]:
            resp = sqs.create_queue(QueueName=f"test-{name}")
            queue_urls[name] = resp["QueueUrl"]

        monkeypatch.setenv("SQS_QUEUE_URL", queue_urls["printer-1"])
        monkeypatch.setenv("SQS_QUEUE_URLS", json.dumps(queue_urls))

        yield {
            "dynamodb": dynamodb,
            "s3": s3,
            "sqs": sqs,
            "queue_urls": queue_urls,
        }
