# Dead Letter Queues
resource "aws_sqs_queue" "dlq" {
  for_each = toset(var.printer_names)

  name                      = "${var.project_name}-${each.value}-dlq"
  message_retention_seconds = 604800 # 7 days
}

# Main Queues
resource "aws_sqs_queue" "printer" {
  for_each = toset(var.printer_names)

  name                       = "${var.project_name}-${each.value}"
  visibility_timeout_seconds = 60
  message_retention_seconds  = 86400 # 1 day
  receive_wait_time_seconds  = 20    # Long polling

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq[each.value].arn
    maxReceiveCount     = 3
  })
}
