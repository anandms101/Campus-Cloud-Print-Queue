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

  default_tags {
    tags = {
      Project     = "CampusCloudPrint"
      Environment = "dev"
      ManagedBy   = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}

# --- Networking ---
module "networking" {
  source       = "./modules/networking"
  project_name = var.project_name
  aws_region   = var.aws_region
}

# --- ECR ---
module "ecr" {
  source       = "./modules/ecr"
  project_name = var.project_name
}

# --- IAM ---
module "iam" {
  source             = "./modules/iam"
  project_name       = var.project_name
  aws_region         = var.aws_region
  account_id         = data.aws_caller_identity.current.account_id
  dynamodb_table_arn = module.dynamodb.table_arn
  s3_bucket_arn      = module.s3.bucket_arn
  sqs_queue_arns     = module.sqs.queue_arns
}

# --- DynamoDB ---
module "dynamodb" {
  source       = "./modules/dynamodb"
  project_name = var.project_name
}

# --- S3 ---
module "s3" {
  source       = "./modules/s3"
  project_name = var.project_name
}

# --- SQS ---
module "sqs" {
  source        = "./modules/sqs"
  project_name  = var.project_name
  printer_names = var.printer_names
}

# --- ALB ---
module "alb" {
  source            = "./modules/alb"
  project_name      = var.project_name
  vpc_id            = module.networking.vpc_id
  public_subnet_ids = module.networking.public_subnet_ids
  alb_sg_id         = module.networking.alb_sg_id
}

# --- CloudWatch ---
module "cloudwatch" {
  source                  = "./modules/cloudwatch"
  project_name            = var.project_name
  aws_region              = var.aws_region
  alb_arn_suffix          = module.alb.alb_arn_suffix
  target_group_arn_suffix = module.alb.target_group_arn_suffix
  printer_names           = var.printer_names
}

# --- ECS ---
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
