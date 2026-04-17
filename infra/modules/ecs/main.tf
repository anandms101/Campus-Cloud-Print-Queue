# -----------------------------------------------------------------------------
# ECS Module
#
# Purpose : Fargate cluster, task definitions, and services for the Go API
#           and per-printer Python workers.
# Inputs  : var.project_name, var.aws_region, networking IDs, IAM role ARNs,
#           container images, storage/queue names, sizing vars, log groups
# Outputs : cluster_name, cluster_arn, api_service_name
# Design  : Fargate eliminates EC2 node management.  containerInsights is
#           enabled for CloudWatch metrics.  The API runs 2 tasks behind the
#           ALB with a 50/200 deployment strategy for zero-downtime rolling
#           updates.  Each printer runs as a standalone 1-task service with
#           0/100 deployment (stop-then-start) because two workers on the
#           same queue would cause duplicate prints.
#           lifecycle { ignore_changes = [task_definition] } on every service
#           lets `make deploy` push new images via the AWS CLI without
#           Terraform reverting the task definition on the next apply.
# -----------------------------------------------------------------------------

resource "aws_ecs_cluster" "main" {
  name = "${var.project_name}-cluster"

  # Feeds per-service CPU/memory metrics into CloudWatch automatically.
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

# ============== API Service ==============

resource "aws_ecs_task_definition" "api" {
  family                   = "${var.project_name}-api"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.api_cpu
  memory                   = var.api_memory
  execution_role_arn       = var.execution_role_arn
  task_role_arn            = var.api_task_role_arn

  container_definitions = jsonencode([{
    name      = "api"
    image     = var.api_image
    essential = true

    portMappings = [{
      containerPort = 8000
      protocol      = "tcp"
    }]

    environment = [
      { name = "DYNAMODB_TABLE", value = var.dynamodb_table_name },
      { name = "S3_BUCKET", value = var.s3_bucket_name },
      { name = "AWS_DEFAULT_REGION", value = var.aws_region },
      {
        name  = "SQS_QUEUE_URLS"
        value = jsonencode(var.sqs_queue_urls)
      }
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = var.api_log_group
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "api"
      }
    }
  }])
}

# 50/200 — starts a new task before stopping the old one (zero-downtime).
resource "aws_ecs_service" "api" {
  name            = "${var.project_name}-api"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.api.arn
  desired_count   = var.api_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.public_subnet_ids
    security_groups  = [var.ecs_sg_id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = var.api_target_group_arn
    container_name   = "api"
    container_port   = 8000
  }

  health_check_grace_period_seconds = 60

  deployment_minimum_healthy_percent = 50
  deployment_maximum_percent         = 200

  # Ignore task_definition so `make deploy` (force-new-deployment) doesn't
  # conflict with Terraform on the next apply.
  lifecycle {
    ignore_changes = [task_definition]
  }
}

# ============== Printer Worker Services ==============

resource "aws_ecs_task_definition" "printer" {
  for_each = toset(var.printer_names)

  family                   = "${var.project_name}-${each.value}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.worker_cpu
  memory                   = var.worker_memory
  execution_role_arn       = var.execution_role_arn
  task_role_arn            = var.printer_task_role_arn

  container_definitions = jsonencode([{
    name      = "printer"
    image     = var.worker_image
    essential = true

    environment = [
      { name = "PRINTER_NAME", value = each.value },
      { name = "SQS_QUEUE_URL", value = var.sqs_queue_urls[each.value] },
      { name = "DYNAMODB_TABLE", value = var.dynamodb_table_name },
      { name = "S3_BUCKET", value = var.s3_bucket_name },
      { name = "AWS_DEFAULT_REGION", value = var.aws_region }
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = var.printer_log_group
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = each.value
      }
    }
  }])
}

# 0/100 (stop-then-start) — each queue must have exactly one consumer
# to prevent duplicate prints.
resource "aws_ecs_service" "printer" {
  for_each = toset(var.printer_names)

  name            = "${var.project_name}-${each.value}"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.printer[each.value].arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.public_subnet_ids
    security_groups  = [var.ecs_sg_id]
    assign_public_ip = true
  }

  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  lifecycle {
    ignore_changes = [task_definition]
  }
}
