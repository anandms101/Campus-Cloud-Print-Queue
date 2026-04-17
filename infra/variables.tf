# -----------------------------------------------------------------------------
# Root Input Variables
#
# Purpose : Centralise every tuneable knob for the campus print infrastructure.
# Notes   : Defaults are sized for a low-cost AWS Academy lab environment.
#           Override via terraform.tfvars or -var flags for production sizing.
# -----------------------------------------------------------------------------

variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-west-2"
}

variable "project_name" {
  description = "Prefix used in every resource name to avoid collisions"
  type        = string
  default     = "campus-print"
}

variable "api_image_tag" {
  description = "Docker image tag for the API service (set by CI or make deploy)"
  type        = string
  default     = "latest"
}

variable "worker_image_tag" {
  description = "Docker image tag for the printer worker (set by CI or make deploy)"
  type        = string
  default     = "latest"
}

variable "printer_names" {
  description = "Logical printer identifiers — one SQS queue and one ECS service per entry"
  type        = list(string)
  default     = ["printer-1", "printer-2", "printer-3"]
}

variable "api_desired_count" {
  description = "Number of API Fargate tasks behind the ALB (2 for HA across AZs)"
  type        = number
  default     = 2
}

variable "api_cpu" {
  description = "CPU units for API task (256 = 0.25 vCPU — sufficient for I/O-bound Go)"
  type        = number
  default     = 256
}

variable "api_memory" {
  description = "Memory in MiB for API task (512 matches 256 CPU Fargate slot)"
  type        = number
  default     = 512
}

variable "worker_cpu" {
  description = "CPU units for printer worker task (256 = 0.25 vCPU)"
  type        = number
  default     = 256
}

variable "worker_memory" {
  description = "Memory in MiB for printer worker task (512 matches 256 CPU Fargate slot)"
  type        = number
  default     = 512
}
