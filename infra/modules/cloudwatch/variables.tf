# -----------------------------------------------------------------------------
# CloudWatch Module — Input Variables
# -----------------------------------------------------------------------------

variable "project_name" {
  type = string
}

variable "aws_region" {
  type = string
}

variable "alb_arn_suffix" {
  type = string
}

variable "target_group_arn_suffix" {
  type = string
}

variable "printer_names" {
  type = list(string)
}
