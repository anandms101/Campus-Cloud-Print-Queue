# -----------------------------------------------------------------------------
# DynamoDB Module
#
# Purpose : Single-table design for print job metadata (status, ownership,
#           timestamps).
# Inputs  : var.project_name
# Outputs : table_name, table_arn
# Design  : PAY_PER_REQUEST billing handles bursty student traffic without
#           capacity planning.  A GSI on (userId, createdAt) supports the
#           "my jobs" query efficiently.  TTL auto-deletes stale jobs so the
#           table doesn't grow unbounded in a lab account.
# -----------------------------------------------------------------------------

resource "aws_dynamodb_table" "jobs" {
  name         = "${var.project_name}-jobs"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "jobId"

  attribute {
    name = "jobId"
    type = "S"
  }

  attribute {
    name = "userId"
    type = "S"
  }

  attribute {
    name = "createdAt"
    type = "S"
  }

  # Supports "all jobs for user X, newest first" without a full table scan.
  global_secondary_index {
    name            = "userId-createdAt-index"
    hash_key        = "userId"
    range_key       = "createdAt"
    projection_type = "ALL"
  }

  # Auto-expires old jobs; the application sets expiresAt on each record.
  ttl {
    attribute_name = "expiresAt"
    enabled        = true
  }
}
