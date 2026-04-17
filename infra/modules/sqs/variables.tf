# -----------------------------------------------------------------------------
# SQS Module — Input Variables
# -----------------------------------------------------------------------------

variable "project_name" {
  type = string
}

variable "printer_names" {
  type = list(string)
}
