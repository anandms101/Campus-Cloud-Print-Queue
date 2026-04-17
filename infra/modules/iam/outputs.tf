# -----------------------------------------------------------------------------
# IAM Module — Outputs
#
# Notes : All three ARNs resolve to the same LabRole. Separate outputs
#         preserve the contract so a future least-privilege refactor can
#         return distinct roles without changing callers.
# -----------------------------------------------------------------------------

output "execution_role_arn" {
  value = data.aws_iam_role.lab_role.arn
}

output "api_task_role_arn" {
  value = data.aws_iam_role.lab_role.arn
}

output "printer_task_role_arn" {
  value = data.aws_iam_role.lab_role.arn
}
