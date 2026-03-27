import uuid
import logging

from fastapi import APIRouter, UploadFile, File, Form, HTTPException, Query
from botocore.exceptions import ClientError

from app.config import VALID_PRINTERS
from app.services import dynamodb, s3, sqs

router = APIRouter(prefix="/jobs", tags=["jobs"])
logger = logging.getLogger("api.jobs")


@router.post("", status_code=201)
async def create_job(
    file: UploadFile = File(...),
    userId: str = Form(...),
):
    job_id = str(uuid.uuid4())
    file_name = file.filename or "document"
    s3_key = f"uploads/{job_id}/{file_name}"

    content = await file.read()
    s3.upload_file(s3_key, content, file.content_type or "application/octet-stream")

    item = dynamodb.create_job(job_id, userId, file_name, s3_key)
    logger.info(f"Job created: {job_id} by user {userId}")

    return _format_job(item)


@router.get("/{job_id}")
def get_job(job_id: str):
    item = dynamodb.get_job(job_id)
    if not item:
        raise HTTPException(status_code=404, detail="Job not found")
    return _format_job(item)


@router.get("")
def list_jobs(
    userId: str = Query(..., description="Filter jobs by user ID"),
    status: str | None = Query(None, description="Filter by status"),
):
    items = dynamodb.list_jobs_by_user(userId, status)
    return [_format_job(item) for item in items]


@router.post("/{job_id}/release")
def release_job(job_id: str, body: dict):
    printer_name = body.get("printerName")
    if not printer_name or printer_name not in VALID_PRINTERS:
        raise HTTPException(
            status_code=400,
            detail=f"Invalid printer. Choose from: {VALID_PRINTERS}",
        )

    # Get job to find s3Key
    item = dynamodb.get_job(job_id)
    if not item:
        raise HTTPException(status_code=404, detail="Job not found")

    try:
        updated = dynamodb.release_job(job_id, printer_name)
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            raise HTTPException(
                status_code=409,
                detail=f"Job cannot be released. Current status: {item.get('status')}",
            )
        raise

    # Enqueue to printer's SQS queue
    sqs.send_job_to_printer(printer_name, job_id, item["s3Key"])
    logger.info(f"Job {job_id} released to {printer_name}")

    return _format_job(updated)


@router.delete("/{job_id}")
def cancel_job(job_id: str):
    item = dynamodb.get_job(job_id)
    if not item:
        raise HTTPException(status_code=404, detail="Job not found")

    try:
        updated = dynamodb.cancel_job(job_id)
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            raise HTTPException(
                status_code=409,
                detail=f"Job cannot be cancelled. Current status: {item.get('status')}",
            )
        raise

    # Best-effort cleanup of S3 document
    try:
        s3.delete_file(item["s3Key"])
    except Exception:
        logger.warning(f"Failed to delete S3 object for job {job_id}")

    logger.info(f"Job {job_id} cancelled")
    return _format_job(updated)


def _format_job(item: dict) -> dict:
    return {
        "jobId": item["jobId"],
        "userId": item["userId"],
        "fileName": item["fileName"],
        "status": item["status"],
        "printerName": item.get("printerName"),
        "createdAt": item["createdAt"],
        "updatedAt": item["updatedAt"],
    }
