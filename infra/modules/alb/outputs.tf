output "alb_dns_name" {
  value = aws_lb.api.dns_name
}

output "alb_arn_suffix" {
  value = aws_lb.api.arn_suffix
}

output "target_group_arn" {
  value = aws_lb_target_group.api.arn
}

output "target_group_arn_suffix" {
  value = aws_lb_target_group.api.arn_suffix
}
