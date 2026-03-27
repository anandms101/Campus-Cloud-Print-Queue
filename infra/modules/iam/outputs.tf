output "execution_role_arn" {
  value = data.aws_iam_role.lab_role.arn
}

output "api_task_role_arn" {
  value = data.aws_iam_role.lab_role.arn
}

output "printer_task_role_arn" {
  value = data.aws_iam_role.lab_role.arn
}
