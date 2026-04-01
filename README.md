# Campus Cloud Print Queue

A distributed print job management system built on AWS, demonstrating release-at-device workflows, optimistic concurrency control, and message-driven processing.

**Team:** Anand Mohan Singh, Vaibhav Thalanki, Pranav Viswanathan

**Introduction Video:** [CampusPrint_Introduction_CS6650.mp4](public/CampusPrint_Introduction_CS6650.mp4)

## Architecture

![System Architecture](public/diagrams/system-overview.png)

**State Machine:**

![State Machine](public/diagrams/state-machine.png)

> See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for full architecture details, data flow diagrams, and design decisions.

## Prerequisites

- **AWS CLI** configured (`aws sts get-caller-identity` should work)
- **Terraform** >= 1.0 (`brew install terraform`)
- **Docker** running (`docker info` should work)
- **Go** 1.25+ (`go version`) — for API development/testing
- **Python** 3.11+ (`python3 --version`) — for printer worker and experiment scripts
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

make go-test          # Run Go API unit tests (with race detector)
make go-vet           # Run Go vet on API code
make go-build         # Build Go API binary locally

make exp1-load        # Experiment 1: Locust load test
make exp2-contention  # Experiment 2: DynamoDB contention
make exp3-saturation  # Experiment 3: Printer saturation
make exp4-fault       # Experiment 4: Fault injection
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (lightweight, for ALB) |
| `GET` | `/health/ready` | Deep health check (verifies DynamoDB, S3, SQS connectivity) |
| `POST` | `/jobs` | Upload a print job (multipart: `file` + `userId`, max 50 MB) |
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
│   ├── api-gin/            # Go Gin REST service (primary API)
│   └── printer-worker/     # SQS-polling processor (Python)
├── tests/
│   ├── unit/                      # Python worker tests (Go API tests live in services/api-gin/)
│   ├── experiment1_load_test/     # Locust load test
│   ├── experiment2_contention/    # DynamoDB conditional write test
│   ├── experiment3_saturation/    # Queue backpressure test
│   └── experiment4_fault_injection/ # Kill + recovery test
├── scripts/                # Seed data, health checks
└── docs/                   # Architecture doc, Mermaid sources, report
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

1. **Go Gin API** with resilience patterns — bulkhead (semaphore-limited uploads), circuit breakers (DynamoDB/S3/SQS), rate limiting, graceful shutdown
2. **Per-printer SQS queues** (not a global queue) — routes by user intent, avoids head-of-line blocking
3. **Fixed-capacity workers** (desired count = 1) — models physical printers, enables saturation study
4. **Optimistic concurrency** via DynamoDB conditional expressions — no distributed locks
5. **Idempotent processing** — conditional state transitions + redelivery handling ensures exactly-once over at-least-once SQS
6. **Compensating transactions** — SQS send failure triggers rollback (RELEASED back to HELD)
7. **Public subnets only** (no NAT Gateway) — saves ~$33/month for a course project
8. **Standard SQS** (not FIFO) — ordering not required, lower cost, higher throughput

## Testing

### Unit tests

**Go API tests** (25 tests, interface-based mocks, race detector enabled):

```bash
cd services/api-gin && go test -v -race -cover ./...
# or from the repo root:
make go-test
```

Tests cover all API endpoints (health, create, get, list, release, cancel), error paths (413, 404, 409), resilience patterns (bulkhead rejection, circuit breaker, compensating rollback), and S3 orphan cleanup.

**Python worker tests** (moto-mocked AWS):

```bash
pip install -r requirements-dev.txt
pytest tests/unit/test_worker.py -v
```

Tests cover the worker state machine (idempotency, redelivery, failure handling).

### CI

GitHub Actions runs on every push and PR to `main`:
- Go vet + build + 25 unit tests (race detector)
- Python worker compile + unit tests (moto)
- Terraform fmt + validate
- Docker build for all images

### Experiment tests

Run against a deployed stack:

```bash
make exp1-load        # Locust: 50 users, 60s
make exp2-contention  # Concurrent release race
make exp3-saturation  # Overload one printer
make exp4-fault       # Kill task, verify recovery
```

## Load Test Results

Locust load test with **50 concurrent users** over **60 seconds** — 1,484 requests, **0% failure rate**, ~25 req/s sustained throughput.

![Load Test Summary](public/diagrams/locust-summary.png)

**Throughput over time** — RPS ramp-up as users join, stabilizing at ~23 req/s:

![RPS Over Time](public/diagrams/locust-rps-over-time.png)

**Response time percentiles** — p50 settles at ~110ms, p99 under 470ms:

![Response Times](public/diagrams/locust-response-times.png)

**Per-endpoint latency breakdown** — POST /jobs is the heaviest (S3 upload), GET endpoints consistently fast:

![Endpoint Latency](public/diagrams/locust-endpoint-latency.png)

## CloudWatch Monitoring

Real-time CloudWatch metrics captured during load test execution:

![CloudWatch Dashboard](public/diagrams/cloudwatch-dashboard.png)

**API log analysis** — endpoint distribution, HTTP status codes, and latency histogram parsed from CloudWatch Logs:

![CloudWatch Logs Analysis](public/diagrams/cloudwatch-logs-analysis.png)

## Known Limitations

This is a course project / proof-of-concept. The following are intentionally out of scope:

- **No authentication/authorization** — any client can create, read, release, or cancel any job. A production system would add JWT/OAuth and per-user ownership checks.
- **No HTTPS** — the ALB listener is HTTP-only. Production would add an ACM certificate and HTTPS listener.
- **No auto-scaling** — printer workers are fixed at 1 task each to model physical devices.

## License

This project is licensed under the [Apache License 2.0](LICENSE).
