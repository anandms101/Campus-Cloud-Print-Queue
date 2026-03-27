import boto3
from app.config import S3_BUCKET, AWS_REGION

_client = boto3.client("s3", region_name=AWS_REGION)


def upload_file(s3_key: str, file_content: bytes, content_type: str = "application/octet-stream"):
    _client.put_object(
        Bucket=S3_BUCKET,
        Key=s3_key,
        Body=file_content,
        ContentType=content_type,
    )


def delete_file(s3_key: str):
    _client.delete_object(Bucket=S3_BUCKET, Key=s3_key)
