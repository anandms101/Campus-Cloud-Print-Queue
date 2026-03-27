import json
import boto3
from app.config import SQS_QUEUE_URLS, AWS_REGION

_client = boto3.client("sqs", region_name=AWS_REGION)


def send_job_to_printer(printer_name: str, job_id: str, s3_key: str):
    queue_url = SQS_QUEUE_URLS.get(printer_name)
    if not queue_url:
        raise ValueError(f"Unknown printer: {printer_name}")

    _client.send_message(
        QueueUrl=queue_url,
        MessageBody=json.dumps({
            "jobId": job_id,
            "s3Key": s3_key,
        }),
    )
