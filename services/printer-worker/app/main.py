import logging
import json
import sys

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


def main():
    logger.info(f"Printer worker starting: {PRINTER_NAME}")
    logger.info(f"Polling queue: {SQS_QUEUE_URL}")

    while True:
        try:
            messages = receive_messages(max_messages=1, wait_time=20)

            if not messages:
                continue

            for msg in messages:
                try:
                    should_delete = process_message(msg)
                    if should_delete:
                        delete_message(msg["ReceiptHandle"])
                except Exception as e:
                    logger.error(f"[{PRINTER_NAME}] Error processing message: {e}", exc_info=True)
                    # Don't delete — SQS will redeliver after visibility timeout

        except KeyboardInterrupt:
            logger.info(f"[{PRINTER_NAME}] Shutting down")
            break
        except Exception as e:
            logger.error(f"[{PRINTER_NAME}] Unexpected error in poll loop: {e}", exc_info=True)


if __name__ == "__main__":
    main()
