# -----------------------------------------------------------------------------
# SQS Module
#
# Purpose : One queue per printer for job routing, each backed by a DLQ for
#           poison-message isolation.
# Inputs  : var.project_name, var.printer_names
# Outputs : queue_urls (map), queue_arns (list), dlq_arns (list)
# Design  : Per-printer queues let the API route a job to a specific printer
#           at release time without a shared dispatcher.  DLQs catch messages
#           that fail 3 times so they don't block the main queue.  Long
#           polling (20 s) reduces empty-receive API calls and cost.
# -----------------------------------------------------------------------------

# DLQ per printer — isolates poison messages for inspection.
resource "aws_sqs_queue" "dlq" {
  for_each = toset(var.printer_names)

  name                      = "${var.project_name}-${each.value}-dlq"
  message_retention_seconds = 604800 # 7 days
}

# visibility_timeout=60s matches the worker's processing budget.
# receive_wait_time=20s enables long polling (reduces empty-receive cost).
resource "aws_sqs_queue" "printer" {
  for_each = toset(var.printer_names)

  name                       = "${var.project_name}-${each.value}"
  visibility_timeout_seconds = 60
  message_retention_seconds  = 86400 # 1 day
  receive_wait_time_seconds  = 20    # long polling

  # After 3 failed receives, message moves to DLQ instead of retrying forever.
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq[each.value].arn
    maxReceiveCount     = 3
  })
}
