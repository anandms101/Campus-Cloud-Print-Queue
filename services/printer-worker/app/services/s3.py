import boto3
from app.config import S3_BUCKET, AWS_REGION

_client = boto3.client("s3", region_name=AWS_REGION)


def download_file(s3_key: str) -> bytes:
    resp = _client.get_object(Bucket=S3_BUCKET, Key=s3_key)
    return resp["Body"].read()


def file_exists(s3_key: str) -> bool:
    try:
        _client.head_object(Bucket=S3_BUCKET, Key=s3_key)
        return True
    except _client.exceptions.ClientError:
        return False
