# -----------------------------------------------------------------------------
# S3 Module
#
# Purpose : Document storage bucket for uploaded print files.
# Inputs  : var.project_name
# Outputs : bucket_name, bucket_arn
# Design  : A random hex suffix guarantees global uniqueness without manual
#           naming.  Public access is fully blocked.  A 1-day lifecycle
#           expiration auto-deletes objects so the lab account never
#           accumulates storage cost.  force_destroy allows `make teardown`
#           to delete the bucket even when it contains objects.
# -----------------------------------------------------------------------------

# Random suffix for globally unique bucket name across student accounts.
resource "random_id" "bucket_suffix" {
  byte_length = 4
}

# force_destroy = true so terraform destroy works without emptying first.
resource "aws_s3_bucket" "documents" {
  bucket        = "${var.project_name}-docs-${random_id.bucket_suffix.hex}"
  force_destroy = true
}

# Block all public access — uploaded documents must never be internet-visible.
resource "aws_s3_bucket_public_access_block" "documents" {
  bucket = aws_s3_bucket.documents.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# 1-day expiration — documents are ephemeral and only need to survive processing.
resource "aws_s3_bucket_lifecycle_configuration" "documents" {
  bucket = aws_s3_bucket.documents.id

  rule {
    id     = "expire-old-docs"
    status = "Enabled"

    filter {}

    expiration {
      days = 1
    }
  }
}
