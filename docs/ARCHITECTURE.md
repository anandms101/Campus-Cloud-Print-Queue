# Campus Cloud Print Queue — System Architecture

## 1. Overview

Campus Cloud Print Queue is a distributed, cloud-native **release-at-device** printing system. Students upload print jobs from any device, and the jobs are held in the cloud until the student physically walks to a printer and releases the job. This decouples submission from execution, eliminating wasted trips, lost prints, and queue confusion.

The system is deployed on AWS using exclusively managed services — no EC2 instances, no self-managed databases, no custom networking appliances.

### System Architecture

![System Overview](../public/diagrams/system-overview.png)

> Mermaid source: [`docs/mermaid/system-overview.mmd`](mermaid/system-overview.mmd)

---

## 2. Component Details

### 2.1 Networking (VPC)

| Resource | Configuration |
|----------|--------------|
| VPC | `10.0.0.0/16`, DNS hostnames enabled |
| Public Subnets | 2 subnets across `us-west-2a` and `us-west-2b` |
| Internet Gateway | Attached to VPC, default route `0.0.0.0/0` |
| NAT Gateway | **None** — all resources in public subnets with public IPs |
| Security Group (ALB) | Ingress: TCP 80 from `0.0.0.0/0`; Egress: all |
| Security Group (ECS) | Ingress: TCP 8000 from ALB SG only; Egress: all |

**Design rationale:** Skipping the NAT Gateway saves ~$33/month. ECS tasks in public subnets with `assign_public_ip = true` reach AWS services directly over the internet. The ECS security group restricts inbound traffic to ALB-originated requests only. Printer workers have no listening ports and are not reachable from the internet.

### 2.2 Application Load Balancer (ALB)

- Internet-facing, deployed across both public subnets
- Single HTTP listener on port 80, forwards to API target group
- Target group: IP-based (Fargate `awsvpc` networking), port 8000
- Health check: `GET /health` every 30s, 2 healthy / 3 unhealthy threshold
- Deregistration delay: 30s (fast rollout during deploys)

### 2.3 ECS Cluster & Services

The cluster `campus-print-cluster` runs 5 Fargate tasks across 4 services:

| Service | Tasks | CPU | Memory | ALB | Scaling |
|---------|-------|-----|--------|-----|---------|
| `campus-print-api` | 2 | 0.25 vCPU | 512 MiB | Yes | Manual (desired=2) |
| `campus-print-printer-1` | 1 | 0.25 vCPU | 512 MiB | No | Fixed (desired=1) |
| `campus-print-printer-2` | 1 | 0.25 vCPU | 512 MiB | No | Fixed (desired=1) |
| `campus-print-printer-3` | 1 | 0.25 vCPU | 512 MiB | No | Fixed (desired=1) |

**API Service** — Stateless REST API (Python FastAPI on Uvicorn). All state lives in DynamoDB and S3. Two tasks load-balanced by the ALB. Health check grace period: 60s.

**Printer Services** — Each printer is a standalone ECS service with `desired_count = 1`. Printers are intentionally **not auto-scaled** because they model physical devices with fixed capacity. If a task crashes, ECS automatically launches a replacement. Printer tasks have no load balancer — they only communicate outbound to SQS, S3, and DynamoDB.

### 2.4 API Service (FastAPI)

| Endpoint | Method | Operation | State Change |
|----------|--------|-----------|-------------|
| `/jobs` | POST | Accept multipart upload (file + userId), store in S3, create DynamoDB item | → `HELD` |
| `/jobs/{id}` | GET | Read from DynamoDB by jobId | (none) |
| `/jobs?userId=X` | GET | Query GSI `userId-createdAt-index`, optional status filter | (none) |
| `/jobs/{id}/release` | POST | Conditional update `HELD→RELEASED`, enqueue to printer's SQS queue | `HELD` → `RELEASED` |
| `/jobs/{id}` | DELETE | Conditional update `HELD→CANCELLED`, delete S3 object | `HELD` → `CANCELLED` |
| `/health` | GET | Return `{"status": "healthy"}` | (none) |

**Middleware:** Every request gets a UUID `X-Request-ID` header. All requests are logged as structured JSON: method, path, status code, duration, request ID.

### 2.5 Printer Worker

![Worker Flowchart](../public/diagrams/worker-flowchart.png)

> Mermaid source: [`docs/mermaid/worker-flowchart.mmd`](mermaid/worker-flowchart.mmd)

**Idempotency design:** SQS provides at-least-once delivery. When a message is redelivered (e.g., after a worker crash), the worker checks the job's current state:
- If `RELEASED`: normal processing path (claim with conditional write)
- If `PROCESSING` with `receiveCount > 1`: re-process (previous worker crashed mid-flight)
- If `DONE`: delete message, skip (already completed)

This was validated by Experiment 4 (fault injection): after killing a printer task mid-processing, all 10 jobs recovered to DONE with zero duplicates.

### 2.6 DynamoDB

**Table: `campus-print-jobs`**

| Attribute | Type | Description |
|-----------|------|-------------|
| `jobId` | String (PK) | UUID, generated at upload time |
| `userId` | String | Uploader's user ID |
| `fileName` | String | Original filename |
| `s3Key` | String | S3 object key (`uploads/{jobId}/{fileName}`) |
| `printerName` | String | Target printer (set at release) |
| `status` | String | `HELD` / `RELEASED` / `PROCESSING` / `DONE` / `CANCELLED` / `FAILED` |
| `createdAt` | String | ISO 8601 timestamp |
| `updatedAt` | String | ISO 8601 timestamp |
| `expiresAt` | Number | Unix epoch (TTL — auto-delete after 24h) |
| `version` | Number | Incremented on each update (optimistic concurrency) |

**GSI: `userId-createdAt-index`** — Partition key: `userId`, Sort key: `createdAt` (descending). Projection: ALL. Enables listing jobs by user, sorted newest first.

**Billing:** PAY_PER_REQUEST (on-demand). Free tier covers 25 WCU / 25 RCU.

**Conditional expression example (release):**
```python
table.update_item(
    Key={"jobId": job_id},
    UpdateExpression="SET #s = :released, printerName = :printer, version = version + :one",
    ConditionExpression="#s = :held",
    ExpressionAttributeValues={":released": "RELEASED", ":held": "HELD", ":printer": "printer-1", ":one": 1}
)
```
If the job is not in HELD state, DynamoDB rejects with `ConditionalCheckFailedException` → API returns HTTP 409 Conflict.

### 2.7 S3

- Bucket: `campus-print-docs-{random_suffix}` (globally unique)
- Object key pattern: `uploads/{jobId}/{fileName}`
- Lifecycle: all objects expire after 1 day
- Public access: fully blocked
- `force_destroy = true` for clean Terraform teardown

### 2.8 SQS

**Per-printer queues (3 main + 3 DLQ):**

| Setting | Value | Rationale |
|---------|-------|-----------|
| Queue type | Standard | Ordering not required, higher throughput |
| Visibility timeout | 60s | Must exceed max print time (15s) + buffer |
| Receive wait time | 20s | Long polling reduces empty responses |
| Message retention | 1 day | Jobs are ephemeral |
| DLQ max receives | 3 | After 3 failed attempts, move to DLQ |
| DLQ retention | 7 days | For investigation |

**Why per-printer queues instead of a global queue:** A global queue creates head-of-line blocking — a slow printer delays jobs for all printers. Per-printer queues give natural partitioning, independent failure domains, and simpler consumer logic. The tradeoff is more resources (6 queues total), but SQS queues are free to create.

### 2.9 CloudWatch

**Log groups (7-day retention):**
- `/ecs/campus-print-api` — API request logs (method, path, status, duration, request ID)
- `/ecs/campus-print-printer` — Worker logs (job ID, state transitions, errors)

**Dashboard panels:**
1. ALB Request Count & Latency (p50, p95, p99)
2. ALB HTTP 4xx / 5xx Error Count
3. SQS Queue Depth per Printer
4. DynamoDB Consumed Read/Write Capacity
5. ECS CPU Utilization (API service)
6. ECS Memory Utilization (API service)

---

## 3. Data Flows

### 3.1 Upload Flow

![Upload Flow](../public/diagrams/upload-flow.png)

> Mermaid source: [`docs/mermaid/upload-flow.mmd`](mermaid/upload-flow.mmd)

The API generates a UUID, uploads the document to S3, and creates a DynamoDB item with `status=HELD`. The job sits in HELD state until the student releases it.

### 3.2 Release Flow

![Release Flow](../public/diagrams/release-flow.png)

> Mermaid source: [`docs/mermaid/release-flow.mmd`](mermaid/release-flow.mmd)

The release endpoint uses a conditional expression (`status = HELD`) to atomically transition to RELEASED. Only if the condition succeeds does the API enqueue to the printer's SQS queue. This prevents double-release.

### 3.3 Processing Flow

![Processing Flow](../public/diagrams/processing-flow.png)

> Mermaid source: [`docs/mermaid/processing-flow.mmd`](mermaid/processing-flow.mmd)

The worker uses conditional writes at each transition. If `RELEASED→PROCESSING` fails (duplicate delivery), the worker deletes the message without processing — making it **idempotent**.

### 3.4 Fault Recovery Flow

![Fault Recovery](../public/diagrams/fault-recovery.png)

> Mermaid source: [`docs/mermaid/fault-recovery.mmd`](mermaid/fault-recovery.mmd)

When a worker crashes: (1) ECS detects the missing task and launches a replacement, (2) SQS messages become visible after the 60s visibility timeout, (3) the new worker re-processes stuck jobs.

---

## 4. State Machine

![State Machine](../public/diagrams/state-machine.png)

> Mermaid source: [`docs/mermaid/state-machine.mmd`](mermaid/state-machine.mmd)

**Every transition is enforced by a DynamoDB conditional expression.** No state can be skipped, reversed, or duplicated.

| Transition | Condition | Actor | HTTP Response |
|-----------|-----------|-------|--------------|
| → HELD | (new item) | API | 201 Created |
| HELD → RELEASED | `status = HELD` | API | 200 OK |
| HELD → CANCELLED | `status = HELD` | API | 200 OK |
| RELEASED → PROCESSING | `status = RELEASED` | Worker | (internal) |
| PROCESSING → DONE | `status = PROCESSING` | Worker | (internal) |
| PROCESSING → FAILED | `status = PROCESSING` | Worker | (internal) |

---

## 5. Key Design Decisions

### 5.1 Per-Printer Queues vs Global Queue

| Aspect | Per-Printer Queues (chosen) | Global Queue |
|--------|---------------------------|--------------|
| Routing | Implicit (one queue = one printer) | Explicit (message attributes + filtering) |
| Isolation | Slow printer doesn't affect others | Head-of-line blocking possible |
| Complexity | More queues (6 total), Terraform `for_each` | Fewer queues, complex consumer logic |
| Load balancing | None (user chooses printer) | Could auto-route to least-loaded |

We chose per-printer queues because the user explicitly selects the printer at release time.

### 5.2 Fixed-Capacity Workers

Printers are **not auto-scaled** (`desired_count = 1`). This models physical reality: you cannot spin up more printers. Makes saturation experiments meaningful.

### 5.3 Optimistic Concurrency Control

DynamoDB conditional expressions replace distributed locks. No lock management, no deadlocks, no coordination service. Under high contention, writes fail and return 409 — acceptable up to 50 concurrent writers (547ms avg latency).

### 5.4 Standard SQS + Idempotent Workers

Standard SQS (not FIFO) + idempotent workers because: print job ordering is irrelevant, standard queues have higher throughput (~unlimited vs 300 msg/s FIFO), idempotency is required regardless.

---

## 6. Infrastructure as Code

### 6.1 Terraform Modules

```
infra/
├── main.tf              # Module composition + provider config
├── variables.tf         # Project name, region, task counts, image tags
├── outputs.tf           # ALB DNS, ECR URLs, table name, queue URLs
└── modules/
    ├── networking/       # VPC, 2 subnets, IGW, route tables, SGs
    ├── ecr/              # 2 repos (api + worker), lifecycle policies
    ├── iam/              # LabRole data source (AWS Academy)
    ├── dynamodb/         # Jobs table, GSI, TTL
    ├── s3/               # Document bucket, lifecycle, access block
    ├── sqs/              # 3 queues + 3 DLQs via for_each
    ├── alb/              # ALB, target group, HTTP listener
    ├── ecs/              # Cluster, API service, 3 printer services
    └── cloudwatch/       # Log groups, dashboard
```

### 6.2 Deployment

```bash
make deploy-fresh    # Full deploy from scratch (~5 min)
make deploy          # Redeploy code changes
make teardown        # Destroy everything
make status          # Show ECS service status
make test-e2e        # Upload → release → wait → verify DONE
```

### 6.3 Cost

| Resource | Cost/hr | Monthly (24/7) |
|----------|---------|---------------|
| ALB | $0.022 | $16 |
| Fargate API (2 tasks) | $0.024 | $18 |
| Fargate Printers (3 tasks) | $0.036 | $27 |
| DynamoDB, SQS, S3, CloudWatch | ~$0 | ~$0 |
| **Total** | **$0.082** | **~$61** |

**Teardown strategy:** `make teardown` destroys everything. Redeploy takes ~5 minutes.
