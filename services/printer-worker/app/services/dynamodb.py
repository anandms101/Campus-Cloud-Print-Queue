import logging

import boto3
from datetime import datetime, timezone
from botocore.exceptions import ClientError

from app.config import DYNAMODB_TABLE, AWS_REGION

logger = logging.getLogger("printer.dynamodb")

_client = boto3.resource("dynamodb", region_name=AWS_REGION)
_table = _client.Table(DYNAMODB_TABLE)


def get_status(job_id: str) -> str | None:
    """Get the current status of a job."""
    resp = _table.get_item(Key={"jobId": job_id}, ProjectionExpression="#s", ExpressionAttributeNames={"#s": "status"})
    item = resp.get("Item")
    return item["status"] if item else None


def mark_processing(job_id: str) -> bool:
    """Conditional update: RELEASED -> PROCESSING. Returns False if condition fails (idempotency)."""
    now = datetime.now(timezone.utc).isoformat()
    try:
        _table.update_item(
            Key={"jobId": job_id},
            UpdateExpression="SET #s = :new_status, updatedAt = :now, version = version + :inc",
            ConditionExpression="#s = :expected",
            ExpressionAttributeNames={"#s": "status"},
            ExpressionAttributeValues={
                ":new_status": "PROCESSING",
                ":expected": "RELEASED",
                ":now": now,
                ":inc": 1,
            },
        )
        return True
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            return False
        raise


def mark_reprocessing(job_id: str) -> int:
    """Refresh updatedAt and increment reprocess_count for a stuck PROCESSING job. Returns the new reprocess_count."""
    now = datetime.now(timezone.utc).isoformat()
    resp = _table.update_item(
        Key={"jobId": job_id},
        UpdateExpression="SET updatedAt = :now, reprocess_count = if_not_exists(reprocess_count, :zero) + :inc",
        ExpressionAttributeValues={
            ":now": now,
            ":zero": 0,
            ":inc": 1,
        },
        ReturnValues="UPDATED_NEW",
    )
    return int(resp["Attributes"].get("reprocess_count", 1))


def mark_done(job_id: str) -> bool:
    """Conditional update: PROCESSING -> DONE. Returns False if condition fails."""
    now = datetime.now(timezone.utc).isoformat()
    try:
        _table.update_item(
            Key={"jobId": job_id},
            UpdateExpression="SET #s = :new_status, updatedAt = :now, version = version + :inc",
            ConditionExpression="#s = :expected",
            ExpressionAttributeNames={"#s": "status"},
            ExpressionAttributeValues={
                ":new_status": "DONE",
                ":expected": "PROCESSING",
                ":now": now,
                ":inc": 1,
            },
        )
        return True
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            return False
        raise


def mark_failed(job_id: str):
    """Best-effort update to FAILED state."""
    now = datetime.now(timezone.utc).isoformat()
    try:
        _table.update_item(
            Key={"jobId": job_id},
            UpdateExpression="SET #s = :new_status, updatedAt = :now",
            ConditionExpression="#s = :expected",
            ExpressionAttributeNames={"#s": "status"},
            ExpressionAttributeValues={
                ":new_status": "FAILED",
                ":expected": "PROCESSING",
                ":now": now,
            },
        )
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            logger.warning("mark_failed skipped for %s: status already changed", job_id)
        else:
            logger.error("mark_failed DynamoDB error for %s: %s", job_id, e)
