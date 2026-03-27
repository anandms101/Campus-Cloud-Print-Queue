import boto3
from datetime import datetime, timezone, timedelta
from botocore.exceptions import ClientError

from app.config import DYNAMODB_TABLE, AWS_REGION

_client = boto3.resource("dynamodb", region_name=AWS_REGION)
_table = _client.Table(DYNAMODB_TABLE)


def create_job(job_id: str, user_id: str, file_name: str, s3_key: str) -> dict:
    now = datetime.now(timezone.utc).isoformat()
    expires = int((datetime.now(timezone.utc) + timedelta(hours=24)).timestamp())

    item = {
        "jobId": job_id,
        "userId": user_id,
        "fileName": file_name,
        "s3Key": s3_key,
        "status": "HELD",
        "printerName": None,
        "createdAt": now,
        "updatedAt": now,
        "expiresAt": expires,
        "version": 1,
    }
    _table.put_item(Item=item)
    return item


def get_job(job_id: str) -> dict | None:
    resp = _table.get_item(Key={"jobId": job_id})
    return resp.get("Item")


def list_jobs_by_user(user_id: str, status: str | None = None) -> list[dict]:
    kwargs = {
        "IndexName": "userId-createdAt-index",
        "KeyConditionExpression": "userId = :uid",
        "ExpressionAttributeValues": {":uid": user_id},
        "ScanIndexForward": False,
    }
    if status:
        kwargs["FilterExpression"] = "#s = :st"
        kwargs["ExpressionAttributeNames"] = {"#s": "status"}
        kwargs["ExpressionAttributeValues"][":st"] = status

    resp = _table.query(**kwargs)
    return resp.get("Items", [])


def release_job(job_id: str, printer_name: str) -> dict:
    """Conditional update: HELD -> RELEASED. Raises ClientError on conflict."""
    now = datetime.now(timezone.utc).isoformat()
    resp = _table.update_item(
        Key={"jobId": job_id},
        UpdateExpression="SET #s = :new_status, printerName = :printer, updatedAt = :now, version = version + :inc",
        ConditionExpression="#s = :expected",
        ExpressionAttributeNames={"#s": "status"},
        ExpressionAttributeValues={
            ":new_status": "RELEASED",
            ":printer": printer_name,
            ":expected": "HELD",
            ":now": now,
            ":inc": 1,
        },
        ReturnValues="ALL_NEW",
    )
    return resp["Attributes"]


def cancel_job(job_id: str) -> dict:
    """Conditional update: HELD -> CANCELLED. Raises ClientError on conflict."""
    now = datetime.now(timezone.utc).isoformat()
    resp = _table.update_item(
        Key={"jobId": job_id},
        UpdateExpression="SET #s = :new_status, updatedAt = :now, version = version + :inc",
        ConditionExpression="#s = :expected",
        ExpressionAttributeNames={"#s": "status"},
        ExpressionAttributeValues={
            ":new_status": "CANCELLED",
            ":expected": "HELD",
            ":now": now,
            ":inc": 1,
        },
        ReturnValues="ALL_NEW",
    )
    return resp["Attributes"]
