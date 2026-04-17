# -----------------------------------------------------------------------------
# ALB Module
#
# Purpose : Internet-facing Application Load Balancer that fronts the API
#           service and provides health-checked traffic routing.
# Inputs  : var.project_name, var.vpc_id, var.public_subnet_ids, var.alb_sg_id
# Outputs : alb_dns_name, alb_arn_suffix, target_group_arn,
#           target_group_arn_suffix
# Design  : target_type = "ip" is required by Fargate awsvpc networking.
#           /health endpoint is checked every 30 s with a fast 2-healthy /
#           3-unhealthy threshold so failing tasks are replaced quickly.
#           deregistration_delay = 30 s (vs. default 300 s) speeds up
#           rolling deploys — acceptable because API requests are short-lived.
#           arn_suffix outputs feed directly into CloudWatch dashboard metrics.
# -----------------------------------------------------------------------------

resource "aws_lb" "api" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [var.alb_sg_id]
  subnets            = var.public_subnet_ids
}

# target_type "ip" is mandatory for Fargate awsvpc networking.
resource "aws_lb_target_group" "api" {
  name        = "${var.project_name}-api-tg"
  port        = 8000
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  # 2/3 thresholds — fast promotion, reasonable tolerance for transient errors.
  health_check {
    path                = "/health"
    port                = "traffic-port"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 30
    timeout             = 5
    matcher             = "200"
  }

  # 30s vs default 300s — API calls are sub-second, no need for long drains.
  deregistration_delay = 30
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.api.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }
}
