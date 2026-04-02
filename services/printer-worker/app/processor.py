import time
import random
import logging
from dataclasses import dataclass

from app.config import MIN_PRINT_TIME, MAX_PRINT_TIME, PRINTER_NAME
from app.services import dynamodb, s3, sqs

logger = logging.getLogger("printer.processor")

_HEARTBEAT_INTERVAL = 45  # seconds between visibility extensions
_VISIBILITY_TIMEOUT = 60  # seconds to extend visibility by


@dataclass
class PrintJob:
    job_id: str
    s3_key: str
    receive_count: int
    receipt_handle: str


def _heartbeat_sleep(duration: float, receipt_handle: str):
    """Sleep for duration seconds, extending SQS visibility every _HEARTBEAT_INTERVAL seconds."""
    elapsed = 0.0
    chunk = 5.0
    last_heartbeat = 0.0
    while elapsed < duration:
        sleep_for = min(chunk, duration - elapsed)
        time.sleep(sleep_for)
        elapsed += sleep_for
        if elapsed - last_heartbeat >= _HEARTBEAT_INTERVAL:
            sqs.extend_visibility(receipt_handle, timeout=_VISIBILITY_TIMEOUT)
            last_heartbeat = elapsed


def process_message(job: PrintJob) -> bool:
    """
    Process a single print job. Returns True if the message should be deleted
    (either successfully processed or a no-op due to idempotency).
    """
    logger.info("[%s] Received job %s (receive count: %s)", PRINTER_NAME, job.job_id, job.receive_count)

    # Step 1: Idempotency check — conditional update RELEASED -> PROCESSING
    if not dynamodb.mark_processing(job.job_id):
        # Check if job is stuck in PROCESSING (e.g., previous worker crashed mid-flight)
        # On redelivery (receive_count > 1), re-process PROCESSING jobs to ensure completion
        current_status = dynamodb.get_status(job.job_id)
        if current_status == "PROCESSING" and job.receive_count > 1:
            reprocess_count = dynamodb.mark_reprocessing(job.job_id)
            logger.warning("[%s] Job %s stuck in PROCESSING (redelivery) — re-processing (reprocess_count: %s)", PRINTER_NAME, job.job_id, reprocess_count)
        elif current_status == "DONE":
            logger.info("[%s] Job %s already DONE — skipping (idempotent no-op)", PRINTER_NAME, job.job_id)
            return True
        else:
            logger.info("[%s] Job %s in state %s — skipping", PRINTER_NAME, job.job_id, current_status)
            return True

    # Step 2: Verify document exists in S3
    try:
        doc = s3.download_file(job.s3_key)
        doc_size = len(doc)
        logger.info("[%s] Downloaded %s (%s bytes)", PRINTER_NAME, job.s3_key, doc_size)
    except Exception as e:
        logger.error("[%s] Failed to download %s: %s", PRINTER_NAME, job.s3_key, e)
        dynamodb.mark_failed(job.job_id)
        return True  # Delete message — retrying won't fix a missing file

    # Step 3: Simulate printing
    print_time = random.uniform(MIN_PRINT_TIME, MAX_PRINT_TIME)
    logger.info("[%s] Printing job %s (%.1fs)", PRINTER_NAME, job.job_id, print_time)
    _heartbeat_sleep(print_time, job.receipt_handle)

    # Step 4: Mark as done
    if dynamodb.mark_done(job.job_id):
        logger.info("[%s] Job %s completed successfully", PRINTER_NAME, job.job_id)
        try:
            s3.delete_file(job.s3_key)
            logger.info("[%s] Deleted S3 object %s for job %s", PRINTER_NAME, job.s3_key, job.job_id)
        except Exception as e:
            logger.warning("[%s] Failed to delete S3 object %s for job %s: %s", PRINTER_NAME, job.s3_key, job.job_id, e)
        return True

    # mark_done returned False — two possible scenarios:
    # 1. DynamoDB had a transient failure: item may still be PROCESSING — redelivery will re-print (acceptable)
    # 2. Write succeeded but returned an error: item is DONE — redelivery will hit the idempotency check and no-op correctly
    logger.warning("[%s] Job %s state was unexpected when marking done — will not delete message", PRINTER_NAME, job.job_id)
    return False
