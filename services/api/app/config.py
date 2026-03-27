import os
import json


DYNAMODB_TABLE = os.environ.get("DYNAMODB_TABLE", "campus-print-jobs")
S3_BUCKET = os.environ.get("S3_BUCKET", "campus-print-docs")
AWS_REGION = os.environ.get("AWS_DEFAULT_REGION", "us-west-2")

# SQS_QUEUE_URLS is a JSON map: {"printer-1": "https://...", "printer-2": "https://...", ...}
_raw = os.environ.get("SQS_QUEUE_URLS", "{}")
SQS_QUEUE_URLS: dict[str, str] = json.loads(_raw) if _raw else {}

VALID_PRINTERS = list(SQS_QUEUE_URLS.keys()) if SQS_QUEUE_URLS else ["printer-1", "printer-2", "printer-3"]
