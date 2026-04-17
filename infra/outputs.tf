# -----------------------------------------------------------------------------
# Root Outputs
#
# Purpose : Expose key resource identifiers consumed by the Makefile, CI, and
#           human operators (e.g., ALB URL for health checks, ECR URLs for
#           docker push, cluster name for `aws ecs` commands).
# -----------------------------------------------------------------------------

output "alb_dns_name" {
  description = "Public DNS of the ALB — the single entry point for all API traffic"
  value       = module.alb.alb_dns_name
}

output "api_ecr_url" {
  description = "ECR repository URL for the API image (used by make push-api)"
  value       = module.ecr.api_repository_url
}

output "worker_ecr_url" {
  description = "ECR repository URL for the printer worker image (used by make push-worker)"
  value       = module.ecr.worker_repository_url
}

output "dynamodb_table_name" {
  description = "DynamoDB jobs table name injected into ECS task environment"
  value       = module.dynamodb.table_name
}

output "s3_bucket_name" {
  description = "S3 document bucket name injected into ECS task environment"
  value       = module.s3.bucket_name
}

output "sqs_queue_urls" {
  description = "Map of printer name to SQS queue URL for job routing"
  value       = module.sqs.queue_urls
}

output "ecs_cluster_name" {
  description = "ECS cluster name used by make deploy and make status"
  value       = module.ecs.cluster_name
}
