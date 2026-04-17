# -----------------------------------------------------------------------------
# Networking Module
#
# Purpose : VPC, public subnets, internet gateway, route tables, and security
#           groups for the campus print queue.
# Inputs  : var.project_name, var.aws_region
# Outputs : vpc_id, public_subnet_ids, alb_sg_id, ecs_sg_id
# Design  : Public-only, 2-AZ topology with no NAT gateway — keeps cost at
#           zero while Fargate tasks with public IPs can still reach AWS APIs.
#           Security groups are chained: ALB allows inbound 80 from the
#           internet; ECS allows inbound 8000 only from the ALB SG.
# -----------------------------------------------------------------------------

# Dynamic AZ discovery — config works in any region without hard-coded names.
data "aws_availability_zones" "available" {
  state = "available"
}

# /16 gives 65k IPs of headroom; dns_hostnames required for service discovery.
resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.project_name}-igw" }
}

# Two AZs satisfy the ALB multi-AZ requirement and give API HA.
resource "aws_subnet" "public" {
  count                   = 2
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.${count.index + 1}.0/24"
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-${count.index + 1}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route" "internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "public" {
  count          = 2
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# ALB SG — inbound HTTP/80 from internet, unrestricted egress.
resource "aws_security_group" "alb" {
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-alb-sg" }

  # Prevents downtime on SG recreation (name_prefix collision after taint).
  lifecycle {
    create_before_destroy = true
  }
}

# ECS SG — only accepts traffic from the ALB on port 8000.
resource "aws_security_group" "ecs" {
  name_prefix = "${var.project_name}-ecs-"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port       = 8000
    to_port         = 8000
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-ecs-sg" }

  lifecycle {
    create_before_destroy = true
  }
}
