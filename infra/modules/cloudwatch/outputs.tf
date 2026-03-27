output "api_log_group_name" {
  value = aws_cloudwatch_log_group.api.name
}

output "printer_log_group_name" {
  value = aws_cloudwatch_log_group.printer.name
}

output "dashboard_name" {
  value = aws_cloudwatch_dashboard.main.dashboard_name
}
