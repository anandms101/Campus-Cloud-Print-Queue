import boto3
from app.config import S3_BUCKET, AWS_REGION

_client = boto3.client("s3", region_name=AWS_REGION)


def download_file(s3_key: str) -> bytes:
    resp = _client.get_object(Bucket=S3_BUCKET, Key=s3_key)
    return resp["Body"].read()


def delete_file(s3_key: str):
    _client.delete_object(Bucket=S3_BUCKET, Key=s3_key)
