variable "project_name" {
  type = string
}

variable "aws_region" {
  type = string
}

variable "account_id" {
  type = string
}

variable "dynamodb_table_arn" {
  type = string
}

variable "s3_bucket_arn" {
  type = string
}

variable "sqs_queue_arns" {
  type = list(string)
}
