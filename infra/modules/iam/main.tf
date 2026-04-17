# -----------------------------------------------------------------------------
# IAM Module
#
# Purpose : Provide IAM role ARNs for ECS task execution and task roles.
# Inputs  : var.project_name, var.aws_region, var.account_id,
#           var.dynamodb_table_arn, var.s3_bucket_arn, var.sqs_queue_arns
# Outputs : execution_role_arn, api_task_role_arn, printer_task_role_arn
# Design  : AWS Academy / Vocareum environments block IAM role creation.
#           Rather than defining aws_iam_role resources (which would fail),
#           we reference the pre-provisioned LabRole that trusts
#           ecs-tasks.amazonaws.com and carries broad permissions for
#           DynamoDB, S3, SQS, CloudWatch, and ECR.
#           All three output ARNs point to the same LabRole; the variable
#           inputs (table ARN, bucket ARN, queue ARNs) are accepted but
#           unused so the module signature stays compatible with a future
#           least-privilege implementation.
# -----------------------------------------------------------------------------

# Data source — LabRole is pre-provisioned in every Academy account.
data "aws_iam_role" "lab_role" {
  name = "LabRole"
}
