import logging
import json
import signal
import sys
import time

from app.config import PRINTER_NAME, SQS_QUEUE_URL
from app.services.sqs import receive_messages, delete_message
from app.processor import process_message

# Setup structured logging
handler = logging.StreamHandler(sys.stdout)
handler.setFormatter(logging.Formatter(
    json.dumps({"timestamp": "%(asctime)s", "level": "%(levelname)s", "logger": "%(name)s", "message": "%(message)s"})
))
logging.root.handlers = [handler]
logging.root.setLevel(logging.INFO)

logger = logging.getLogger("printer.main")

_shutdown = False


def _handle_signal(signum, _frame):
    global _shutdown
    logger.info(f"[{PRINTER_NAME}] Received signal {signum}, shutting down gracefully")
    _shutdown = True


def main():
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    logger.info(f"Printer worker starting: {PRINTER_NAME}")
    logger.info(f"Polling queue: {SQS_QUEUE_URL}")

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
                try:
                    should_delete = process_message(msg)
                    if should_delete:
                        delete_message(msg["ReceiptHandle"])
                except Exception as e:
                    logger.error(f"[{PRINTER_NAME}] Error processing message: {e}", exc_info=True)

        except Exception as e:
            consecutive_errors += 1
            backoff = min(2 ** consecutive_errors, 60)
            logger.error(f"[{PRINTER_NAME}] Poll loop error (backoff {backoff}s): {e}", exc_info=True)
            time.sleep(backoff)

    logger.info(f"[{PRINTER_NAME}] Worker stopped")


if __name__ == "__main__":
    main()
