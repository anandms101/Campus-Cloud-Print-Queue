import json
import time
import random
import logging

from app.config import MIN_PRINT_TIME, MAX_PRINT_TIME, PRINTER_NAME
from app.services import dynamodb, s3

logger = logging.getLogger("printer.processor")


def process_message(message: dict) -> bool:
    """
    Process a single SQS message. Returns True if the message should be deleted
    (either successfully processed or a no-op due to idempotency).
    """
    body = json.loads(message["Body"])
    job_id = body["jobId"]
    s3_key = body["s3Key"]
    receive_count = message.get("Attributes", {}).get("ApproximateReceiveCount", "1")

    logger.info(f"[{PRINTER_NAME}] Received job {job_id} (receive count: {receive_count})")

    # Step 1: Idempotency check — conditional update RELEASED -> PROCESSING
    if not dynamodb.mark_processing(job_id):
        # Check if job is stuck in PROCESSING (e.g., previous worker crashed mid-flight)
        # On redelivery (receive_count > 1), re-process PROCESSING jobs to ensure completion
        current_status = dynamodb.get_status(job_id)
        if current_status == "PROCESSING" and int(receive_count) > 1:
            logger.info(f"[{PRINTER_NAME}] Job {job_id} stuck in PROCESSING (redelivery) — re-processing")
        elif current_status == "DONE":
            logger.info(f"[{PRINTER_NAME}] Job {job_id} already DONE — skipping (idempotent no-op)")
            return True
        else:
            logger.info(f"[{PRINTER_NAME}] Job {job_id} in state {current_status} — skipping")
            return True

    # Step 2: Verify document exists in S3
    try:
        doc = s3.download_file(s3_key)
        doc_size = len(doc)
        logger.info(f"[{PRINTER_NAME}] Downloaded {s3_key} ({doc_size} bytes)")
    except Exception as e:
        logger.error(f"[{PRINTER_NAME}] Failed to download {s3_key}: {e}")
        dynamodb.mark_failed(job_id)
        return True  # Delete message — retrying won't fix a missing file

    # Step 3: Simulate printing
    print_time = random.uniform(MIN_PRINT_TIME, MAX_PRINT_TIME)
    logger.info(f"[{PRINTER_NAME}] Printing job {job_id} ({print_time:.1f}s)")
    time.sleep(print_time)

    # Step 4: Mark as done
    if dynamodb.mark_done(job_id):
        logger.info(f"[{PRINTER_NAME}] Job {job_id} completed successfully")
        try:
            s3.delete_file(s3_key)
            logger.info(f"[{PRINTER_NAME}] Deleted S3 object {s3_key} for job {job_id}")
        except Exception as e:
            logger.warning(f"[{PRINTER_NAME}] Failed to delete S3 object {s3_key} for job {job_id}: {e}")
        return True

    logger.warning(f"[{PRINTER_NAME}] Job {job_id} state was unexpected when marking done — will not delete message")
    return False
