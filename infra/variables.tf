variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-west-2"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "campus-print"
}

variable "api_image_tag" {
  description = "Docker image tag for the API service"
  type        = string
  default     = "latest"
}

variable "worker_image_tag" {
  description = "Docker image tag for the printer worker"
  type        = string
  default     = "latest"
}

variable "printer_names" {
  description = "List of printer names"
  type        = list(string)
  default     = ["printer-1", "printer-2", "printer-3"]
}

variable "api_desired_count" {
  description = "Number of API tasks"
  type        = number
  default     = 2
}

variable "api_cpu" {
  description = "CPU units for API task"
  type        = number
  default     = 256
}

variable "api_memory" {
  description = "Memory (MiB) for API task"
  type        = number
  default     = 512
}

variable "worker_cpu" {
  description = "CPU units for printer worker task"
  type        = number
  default     = 256
}

variable "worker_memory" {
  description = "Memory (MiB) for printer worker task"
  type        = number
  default     = 512
}
