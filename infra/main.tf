# -----------------------------------------------------------------------------
# Campus Cloud Print Queue — Root Terraform Configuration
#
# Purpose : Compose nine single-responsibility modules into a complete AWS
#           deployment for the campus print queue system.
# Design  : Each module owns one AWS concern (networking, storage, compute…).
#           The root wires outputs -> inputs between them, keeping each module
#           independently reviewable, testable, and destroyable.
# Order   : Modules are declared in dependency order — networking and stateless
#           resources first, then IAM, then compute (ECS) last.
# Tags    : default_tags ensure every resource is labelled for cost tracking
#           and ownership without repeating tags in each module.
# -----------------------------------------------------------------------------

terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

provider "aws" {
  region = var.aws_region

  # Propagates Project/Environment/ManagedBy to every resource automatically.
  default_tags {
    tags = {
      Project     = "CampusCloudPrint"
      Environment = "dev"
      ManagedBy   = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}

# --- Networking (VPC, subnets, SGs) — must exist before ALB and ECS ---
module "networking" {
  source       = "./modules/networking"
  project_name = var.project_name
  aws_region   = var.aws_region
}

# --- ECR (container registries) — must exist before ECS image references ---
module "ecr" {
  source       = "./modules/ecr"
  project_name = var.project_name
}

# --- IAM — references pre-provisioned LabRole for ECS task execution ---
module "iam" {
  source             = "./modules/iam"
  project_name       = var.project_name
  aws_region         = var.aws_region
  account_id         = data.aws_caller_identity.current.account_id
  dynamodb_table_arn = module.dynamodb.table_arn
  s3_bucket_arn      = module.s3.bucket_arn
  sqs_queue_arns     = module.sqs.queue_arns
}

# --- DynamoDB — job metadata store, ARN/name flow into IAM and ECS ---
module "dynamodb" {
  source       = "./modules/dynamodb"
  project_name = var.project_name
}

# --- S3 — document storage, ARN/name flow into IAM and ECS ---
module "s3" {
  source       = "./modules/s3"
  project_name = var.project_name
}

# --- SQS — one queue per printer for job routing ---
module "sqs" {
  source        = "./modules/sqs"
  project_name  = var.project_name
  printer_names = var.printer_names
}

# --- ALB — single public entry point; exposes arn_suffix to CloudWatch ---
module "alb" {
  source            = "./modules/alb"
  project_name      = var.project_name
  vpc_id            = module.networking.vpc_id
  public_subnet_ids = module.networking.public_subnet_ids
  alb_sg_id         = module.networking.alb_sg_id
}

# --- CloudWatch — log groups (must exist before ECS) + dashboard ---
module "cloudwatch" {
  source                  = "./modules/cloudwatch"
  project_name            = var.project_name
  aws_region              = var.aws_region
  alb_arn_suffix          = module.alb.alb_arn_suffix
  target_group_arn_suffix = module.alb.target_group_arn_suffix
  printer_names           = var.printer_names
}

# --- ECS — declared last; depends on networking, IAM, ECR, storage, logs ---
module "ecs" {
  source                = "./modules/ecs"
  project_name          = var.project_name
  aws_region            = var.aws_region
  vpc_id                = module.networking.vpc_id
  public_subnet_ids     = module.networking.public_subnet_ids
  ecs_sg_id             = module.networking.ecs_sg_id
  api_target_group_arn  = module.alb.target_group_arn
  execution_role_arn    = module.iam.execution_role_arn
  api_task_role_arn     = module.iam.api_task_role_arn
  printer_task_role_arn = module.iam.printer_task_role_arn
  api_image             = "${module.ecr.api_repository_url}:${var.api_image_tag}"
  worker_image          = "${module.ecr.worker_repository_url}:${var.worker_image_tag}"
  dynamodb_table_name   = module.dynamodb.table_name
  s3_bucket_name        = module.s3.bucket_name
  sqs_queue_urls        = module.sqs.queue_urls
  printer_names         = var.printer_names
  api_desired_count     = var.api_desired_count
  api_cpu               = var.api_cpu
  api_memory            = var.api_memory
  worker_cpu            = var.worker_cpu
  worker_memory         = var.worker_memory
  api_log_group         = module.cloudwatch.api_log_group_name
  printer_log_group     = module.cloudwatch.printer_log_group_name
}
