import boto3
from app.config import SQS_QUEUE_URL, AWS_REGION

_client = boto3.client("sqs", region_name=AWS_REGION)


def receive_messages(max_messages: int = 1, wait_time: int = 20) -> list[dict]:
    resp = _client.receive_message(
        QueueUrl=SQS_QUEUE_URL,
        MaxNumberOfMessages=max_messages,
        WaitTimeSeconds=wait_time,
        AttributeNames=["ApproximateReceiveCount"],
    )
    return resp.get("Messages", [])


def delete_message(receipt_handle: str):
    _client.delete_message(
        QueueUrl=SQS_QUEUE_URL,
        ReceiptHandle=receipt_handle,
    )


def extend_visibility(receipt_handle: str, timeout: int = 60):
    _client.change_message_visibility(
        QueueUrl=SQS_QUEUE_URL,
        ReceiptHandle=receipt_handle,
        VisibilityTimeout=timeout,
    )
