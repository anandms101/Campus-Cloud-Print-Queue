# -----------------------------------------------------------------------------
# ALB Module — Outputs
# -----------------------------------------------------------------------------

output "alb_dns_name" {
  value = aws_lb.api.dns_name
}

# arn_suffix is the CloudWatch metric dimension for ALB dashboard widgets.
output "alb_arn_suffix" {
  value = aws_lb.api.arn_suffix
}

output "target_group_arn" {
  value = aws_lb_target_group.api.arn
}

output "target_group_arn_suffix" {
  value = aws_lb_target_group.api.arn_suffix
}
