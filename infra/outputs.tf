output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = module.alb.alb_dns_name
}

output "api_ecr_url" {
  description = "ECR repository URL for the API"
  value       = module.ecr.api_repository_url
}

output "worker_ecr_url" {
  description = "ECR repository URL for the printer worker"
  value       = module.ecr.worker_repository_url
}

output "dynamodb_table_name" {
  description = "DynamoDB table name"
  value       = module.dynamodb.table_name
}

output "s3_bucket_name" {
  description = "S3 bucket name"
  value       = module.s3.bucket_name
}

output "sqs_queue_urls" {
  description = "SQS queue URLs by printer name"
  value       = module.sqs.queue_urls
}

output "ecs_cluster_name" {
  description = "ECS cluster name"
  value       = module.ecs.cluster_name
}
