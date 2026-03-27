import os

PRINTER_NAME = os.environ.get("PRINTER_NAME", "printer-1")
SQS_QUEUE_URL = os.environ.get("SQS_QUEUE_URL", "")
DYNAMODB_TABLE = os.environ.get("DYNAMODB_TABLE", "campus-print-jobs")
S3_BUCKET = os.environ.get("S3_BUCKET", "campus-print-docs")
AWS_REGION = os.environ.get("AWS_DEFAULT_REGION", "us-west-2")

# Simulated print time range (seconds)
MIN_PRINT_TIME = 5
MAX_PRINT_TIME = 15
