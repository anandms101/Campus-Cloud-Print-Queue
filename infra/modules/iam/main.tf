# AWS Academy / Vocareum environments restrict IAM role creation.
# We use the pre-existing LabRole which trusts ecs-tasks.amazonaws.com
# and has broad permissions for DynamoDB, S3, SQS, CloudWatch, ECR, etc.

data "aws_iam_role" "lab_role" {
  name = "LabRole"
}
