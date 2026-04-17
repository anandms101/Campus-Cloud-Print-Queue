# -----------------------------------------------------------------------------
# SQS Module — Outputs
# -----------------------------------------------------------------------------

output "queue_urls" {
  value = { for k, v in aws_sqs_queue.printer : k => v.url }
}

output "queue_arns" {
  value = [for q in aws_sqs_queue.printer : q.arn]
}

output "dlq_arns" {
  value = [for q in aws_sqs_queue.dlq : q.arn]
}
