# Campus Cloud Print Queue

A distributed print job management system built on AWS, demonstrating release-at-device workflows, optimistic concurrency control, and message-driven processing.

**Team:** Pranav Viswanathan, Anand Mohan Singh, Vaibhav Thalanki

## Architecture

![System Architecture](public/diagrams/system-overview.png)

**State Machine:**

![State Machine](public/diagrams/state-machine.png)

> See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for full architecture details, data flow diagrams, and design decisions.

## Prerequisites

- **AWS CLI** configured (`aws sts get-caller-identity` should work)
- **Terraform** >= 1.0 (`brew install terraform`)
- **Docker** running (`docker info` should work)
- **Python** 3.11+ (`python3 --version`)
- **Make** (pre-installed on macOS/Linux)

## Quick Start

### One-command deploy from scratch

```bash
make deploy-fresh
```

This runs: `terraform init` → `terraform apply` → Docker build → ECR push → ECS force-deploy → health check wait.

### Or step by step

```bash
make infra-init       # Initialize Terraform
make infra-plan       # Preview changes
make infra-apply      # Create AWS resources
make push-all         # Build and push Docker images
make deploy           # Force ECS to pull new images
make test-health      # Verify API is live
make test-e2e         # Full upload → release → done test
```

### Tear down (saves money!)

```bash
make teardown
```

## Available Commands

```bash
make help             # Show all commands
make deploy-fresh     # Full deploy from scratch
make deploy           # Redeploy (images + ECS restart)
make teardown         # Destroy everything

make status           # Show all ECS service status
make queue-depth      # Show SQS queue depths
make logs-api         # Tail API logs
make logs-printer     # Tail printer logs

make test-health      # Health check
make test-upload      # Upload a test job
make test-e2e         # Full E2E test

make exp1-load        # Experiment 1: Locust load test
make exp2-contention  # Experiment 2: DynamoDB contention
make exp3-saturation  # Experiment 3: Printer saturation
make exp4-fault       # Experiment 4: Fault injection
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `POST` | `/jobs` | Upload a print job (multipart: `file` + `userId`) |
| `GET` | `/jobs/{id}` | Get job status |
| `GET` | `/jobs?userId=X` | List user's jobs |
| `POST` | `/jobs/{id}/release` | Release to printer (`{"printerName": "printer-1"}`) |
| `DELETE` | `/jobs/{id}` | Cancel a held job |

### Example upload curl

```bash
curl -X POST http://<ALB_DNS>/jobs \
  -F "file=@./report.pdf" \
  -F "userId=student123"
```

### Example release curl

```bash
curl -X POST http://<ALB_DNS>/jobs/<job-id>/release \
  -H "Content-Type: application/json" \
  -d '{"printerName":"printer-2"}'
```

## Project Structure

```
├── Makefile                # Build, deploy, test, teardown
├── infra/                  # Terraform (9 modules)
│   └── modules/
│       ├── networking/     # VPC, 2 public subnets, security groups
│       ├── ecr/            # Container registries (api + worker)
│       ├── iam/            # LabRole reference (AWS Academy)
│       ├── dynamodb/       # Jobs table + GSI + TTL
│       ├── s3/             # Document bucket + lifecycle
│       ├── sqs/            # 3 printer queues + 3 DLQs
│       ├── alb/            # Load balancer + target group
│       ├── ecs/            # Cluster + API (2) + printers (3x1)
│       └── cloudwatch/     # Log groups + dashboard
├── services/
│   ├── api/                # FastAPI REST service (Python)
│   └── printer-worker/     # SQS-polling processor (Python)
├── tests/
│   ├── experiment1_load_test/     # Locust load test
│   ├── experiment2_contention/    # DynamoDB conditional write test
│   ├── experiment3_saturation/    # Queue backpressure test
│   └── experiment4_fault_injection/ # Kill + recovery test
├── scripts/                # Seed data, health checks
└── docs/                   # Architecture doc, report
```

## Cost

Running costs ~$0.08/hour. **Always tear down when not in use:** `make teardown`

| Resource | ~Cost/hr | Notes |
|----------|----------|-------|
| ALB | $0.022 | Fixed cost while running |
| Fargate API (2 tasks) | $0.024 | 0.25 vCPU, 512 MiB each |
| Fargate Printers (3 tasks) | $0.036 | 0.25 vCPU, 512 MiB each |
| DynamoDB, SQS, S3 | ~$0 | Pay-per-request / free tier |

Redeploy from scratch takes ~5 minutes.

## Key Design Decisions

1. **Per-printer SQS queues** (not a global queue) — routes by user intent, avoids head-of-line blocking
2. **Fixed-capacity workers** (desired count = 1) — models physical printers, enables saturation study
3. **Optimistic concurrency** via DynamoDB conditional expressions — no distributed locks
4. **Idempotent processing** — conditional state transitions + redelivery handling ensures exactly-once over at-least-once SQS
5. **Public subnets only** (no NAT Gateway) — saves ~$33/month for a course project
6. **Standard SQS** (not FIFO) — ordering not required, lower cost, higher throughput
