import json
import logging
import signal
import sys
import time

from app.config import PRINTER_NAME, SQS_QUEUE_URL
from app.services.sqs import receive_messages, delete_message
from app.processor import PrintJob, process_message

# Setup structured logging
handler = logging.StreamHandler(sys.stdout)
handler.setFormatter(logging.Formatter(
    json.dumps({"timestamp": "%(asctime)s", "level": "%(levelname)s", "logger": "%(name)s", "message": "%(message)s"})
))
logging.root.handlers = [handler]
logging.root.setLevel(logging.INFO)

logger = logging.getLogger("printer.main")

# Safe under CPython's GIL for this single-threaded loop.
# Should be replaced with threading.Event if the worker is ever made multi-threaded.
_shutdown = False


def _handle_signal(signum, _frame):
    global _shutdown
    logger.info("[%s] Received signal %s, shutting down gracefully", PRINTER_NAME, signum)
    _shutdown = True


def main():
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    logger.info("Printer worker starting: %s", PRINTER_NAME)
    logger.info("Polling queue: %s", SQS_QUEUE_URL)

    consecutive_errors = 0

    while not _shutdown:
        try:
            messages = receive_messages(max_messages=1, wait_time=20)
            consecutive_errors = 0

            if not messages:
                continue

            for msg in messages:
                if _shutdown:
                    break
                body = json.loads(msg["Body"])
                job = PrintJob(
                    job_id=body["jobId"],
                    s3_key=body["s3Key"],
                    receive_count=int(msg.get("Attributes", {}).get("ApproximateReceiveCount", "1")),
                    receipt_handle=msg["ReceiptHandle"],
                )
                try:
                    should_delete = process_message(job)
                    if should_delete:
                        delete_message(job.receipt_handle)
                except Exception as e:
                    logger.error("[%s] Error processing message: %s", PRINTER_NAME, e, exc_info=True)

        except Exception as e:
            consecutive_errors += 1
            backoff = min(2 ** consecutive_errors, 60)
            logger.error("[%s] Poll loop error (backoff %ss): %s", PRINTER_NAME, backoff, e, exc_info=True)
            time.sleep(backoff)

    logger.info("[%s] Worker stopped", PRINTER_NAME)


if __name__ == "__main__":
    main()
