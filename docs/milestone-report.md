# Campus Cloud Print Queue — Milestone 1 Report

**Team:** Pranav Viswanathan, Anand Mohan Singh, Vaibhav Thalanki (Northeastern University)
**Course:** CS 6650 — Building Scalable Distributed Systems | **Date:** March 27, 2026
**Repository:** https://github.com/anandms101/Campus-Cloud-Print-Queue

---

## 1. Problem, Team, and Overview

**Problem.** Campus printing is synchronous and error-prone. Students must physically walk to a printer, send a job, wait for it, and hope it works. If the device jams or runs out of paper, they start over. *Release-at-device printing* decouples submission from execution: students upload jobs from anywhere (held in the cloud), then release them at a specific printer when ready. No wasted trips, no lost prints, no queue confusion.

**System.** We built a cloud-native implementation on AWS: a stateless FastAPI service on ECS/Fargate behind an ALB, DynamoDB for job state with conditional writes for concurrency control, S3 for document storage, and per-printer SQS queues driving fixed-capacity printer workers. The job state machine is: `HELD → RELEASED → PROCESSING → DONE`, with `CANCELLED` and `FAILED` as terminal states.

**Team.** Pranav Viswanathan — API development (FastAPI, 6 endpoints), DynamoDB schema design, conditional write logic. Anand Mohan Singh — Infrastructure (Terraform, 9 modules), deployment pipeline, Makefile automation. Vaibhav Thalanki — Printer worker, experiment scripts (Locust, async Python), CloudWatch observability.

**Experiments.** We designed four experiments to validate distributed systems properties: (1) API load testing with Locust at 50–100 concurrent users, (2) DynamoDB contention testing with 2–50 concurrent conditional writes, (3) printer saturation with 50 jobs flooding a single printer, and (4) fault injection by killing a printer worker mid-processing.

**AI Usage.** We used Claude Code for architecture review, Terraform scaffolding, and boilerplate generation, saving approximately 8 hours. Claude was *not* used for experiment design or result analysis. AI-generated IAM configs required manual rework for AWS Academy's LabRole constraint — every file was reviewed by a team member before merge.

**Observability.** Structured JSON logging to CloudWatch Logs with 7-day retention. A 6-panel CloudWatch dashboard tracks ALB latency (p50/p95/p99), HTTP errors, SQS queue depth per printer, DynamoDB capacity, and ECS CPU/memory utilization. UUID request tracing via `X-Request-ID` headers enables end-to-end correlation.

---

## 2. Project Plan and Progress

| Week | Dates | Focus | Status |
|------|-------|-------|--------|
| 1 | Mar 23–29 | Architecture finalization, AWS setup, Terraform modules | **DONE** |
| 2 | Mar 30–Apr 5 | Core flow: upload, release, process. DynamoDB + SQS integration | **DONE** |
| 3 | Apr 6–12 | Correctness: conditional writes, idempotent workers, error handling | IN PROGRESS |
| 4 | Apr 13–19 | All 4 experiments executed, metrics collected | IN PROGRESS |
| 5 | Apr 20–26 | Final analysis, report, presentation | IN PROGRESS |

**Anand** built 9 Terraform modules (networking, ECR, IAM, DynamoDB, S3, SQS, ALB, ECS, CloudWatch) and a Makefile with 20+ targets including `deploy-fresh` (one-command full deploy), `teardown`, `status`, `queue-depth`, and per-experiment runners.

**Pranav** implemented the FastAPI application with 6 endpoints, designed the DynamoDB schema (PK: `jobId`, GSI: `userId-createdAt-index`, TTL on `expiresAt`), and wrote all conditional expression logic for state transitions.

**Vaibhav** built the printer worker with SQS polling and idempotent redelivery handling, wrote all 4 experiment scripts, and configured CloudWatch log groups and the monitoring dashboard.

**AI cost/benefit.** ~8 hours saved on Terraform boilerplate and Docker configs. Key risk: AI assumed custom IAM roles; AWS Academy requires a shared LabRole. Mitigation: mandatory team review of all AI-generated code.

---

## 3. Objectives

**Short-term.** (1) Working end-to-end system with upload → hold → release → process → done flow. (2) Four experiments with quantitative results validating correctness and performance. (3) Observable system via CloudWatch dashboard and structured logging.

**Long-term.** Open-source reference architecture for release-at-device printing. Reusable observability patterns for ECS microservices. Educational resource — experiments as distributed systems teaching material.

**Future work.** Multi-region deployment (DynamoDB Global Tables), FIFO ordering for collated documents, Fargate Spot for cost savings, real printer integration (IPP/CUPS), university SSO authentication, X-Ray distributed tracing for cross-service latency analysis.

---

## 4. Related Work

**MapReduce (Dean & Ghemawat, 2004).** Our printer workers are analogous to reduce workers processing partitioned data. Jobs are partitioned by printer and processed independently. Unlike MapReduce's static partitioning, ours is dynamic (at release time). Fault tolerance follows the same pattern: re-execute failed tasks via ECS restart + SQS redelivery.

**Dynamo (DeCandia et al., 2007).** Our DynamoDB conditional expressions implement optimistic concurrency control, rooted in Dynamo's design. Writes carry preconditions; conflicts are detected and returned as exceptions rather than corrupting state — analogous to compare-and-swap (CAS).

**Lamport Clocks (1978).** Our `version` field serves as a logical clock per job — each state transition increments it, providing a total order of mutations independent of wall-clock time.

**AWS Well-Architected Framework.** We follow the *Operational Excellence* pillar (IaC via Terraform, structured logging, automated deploys) and *Cost Optimization* pillar (pay-per-request DynamoDB, Fargate, S3 lifecycle policies, no NAT Gateway — saving ~$33/month).

**SQS At-Least-Once Delivery (AWS Docs).** Our idempotent worker pattern follows AWS best practices: conditional state checks before processing, safe duplicate handling, and dead-letter queues for poison messages after 3 failed attempts.

---

## 5. Methodology

**Architecture.** The system has 6 component types on AWS ECS Fargate: ALB → API service (2 tasks) → DynamoDB + S3 + SQS (3 per-printer queues + 3 DLQs) → Printer workers (3 tasks, `desired_count=1`). Every state transition uses a DynamoDB `ConditionExpression` — no state can be skipped, reversed, or duplicated. Full architecture details in `docs/ARCHITECTURE.md`.

**AI in methodology.** Claude generated Terraform scaffolding and FastAPI boilerplate. The team designed the DynamoDB schema, conditional write expressions, idempotency logic, and all experiment scripts independently.

**Observability approach.** Every service emits structured JSON logs. CloudWatch dashboard provides real-time visibility. During experiments, we collect data from three sources: (1) experiment scripts, (2) CloudWatch dashboard, (3) AWS CLI queries.

**Experiment 1 — Load Test.** Locust simulates concurrent users with a weighted request mix: uploads (3), polls (2), releases (1), lists (1). Configurations: 50 users/60s and 100 users/120s. Metrics: RPS, p50/p95/p99 latency, error rate.

**Experiment 2 — Contention.** Create a HELD job, fire N parallel release requests via asyncio/aiohttp (N=2,5,10,20,50). Verify exactly 1 success (HTTP 200), N-1 conflicts (HTTP 409), zero errors.

**Experiment 3 — Saturation.** Release 50 jobs to a single printer. Measure total drain time, per-job latency distribution (min, p50, p95, max), verify zero job loss.

**Experiment 4 — Fault Injection.** Release 10 jobs, wait until 1+ is PROCESSING, kill the printer task with `aws ecs stop-task`. Verify ECS restart, SQS redelivery, all jobs reach DONE, zero duplicates.

**Tradeoffs evaluated.** Per-printer queues vs global (validated by Exp 3), fixed vs elastic workers (Exp 3), optimistic vs pessimistic concurrency (Exp 2), Standard SQS + idempotency vs FIFO (Exp 4).

---

## 6. Preliminary Results

### Experiment 1: API Load Test

| Config | Requests | Errors | Throughput |
|--------|----------|--------|------------|
| 50 users, 60s | 1,516 | 0 | 25.6 req/s |
| 100 users, 120s | 5,492 | 0 | 46.4 req/s |

**Latency (100 users):** `POST /jobs` p50=160ms, p95=380ms, p99=770ms. `GET /jobs/{id}` p50=120ms, p95=370ms. `POST /release` p50=150ms, p95=490ms. Throughput scales ~linearly (25.6 → 46.4 req/s as users double), confirming the stateless design distributes load effectively. Upload is the slowest operation (S3 + DynamoDB write). Zero errors across 5,492 requests confirms API stability.

### Experiment 2: DynamoDB Contention

| N | Success | Conflict (409) | Errors | Avg Latency | Pass? |
|---|---------|----------------|--------|-------------|-------|
| 2 | 1 | 1 | 0 | 239ms | **PASS** |
| 5 | 1 | 4 | 0 | 300ms | **PASS** |
| 10 | 1 | 9 | 0 | 239ms | **PASS** |
| 20 | 1 | 19 | 0 | 418ms | **PASS** |
| 50 | 1 | 49 | 0 | 547ms | **PASS** |

Exactly one winner at all levels. Zero errors. Latency grows sub-linearly (2.3x from N=2 to N=50). DynamoDB conditional expressions correctly enforce the exactly-one-release invariant even under extreme contention.

### Experiment 3: Printer Saturation (50 jobs → printer-1)

All 50 jobs completed (100%), 0 timeouts. Release time: 2.3s (API/SQS not the bottleneck). Latency: min 76s, p50 363s, p95 500s, max 515s. The single worker processes ~1 job every 10s (5–15s simulated print time), so 50 jobs take ~500s — matching the observed max. **Queue depth is the primary latency driver** with fixed-capacity workers, modeling real printer behavior.

### Experiment 4: Fault Injection

10 jobs released, 1 in PROCESSING when task killed. ECS launched a replacement (~60s), SQS redelivered after visibility timeout (~60s). All 10 jobs reached DONE. Zero duplicates. Recovery time: 154s. *Note:* our initial run revealed a bug — the worker treated PROCESSING jobs as "already handled" and skipped them, leaving crash-in-progress jobs stuck. We fixed the idempotency logic to re-process stuck PROCESSING jobs on redelivery, then achieved 10/10 recovery. This bug discovery demonstrates why fault injection testing is essential.

### What Remains and Worst-Case Analysis

**Remaining work.** Run at 200/500 users to find the throughput ceiling. Multi-printer saturation with 100+ jobs. API failover testing. Correlate CloudWatch metrics with experiment timelines for the final report.

**Worst-case workload.** (1) *Burst to single printer:* 500 jobs → 1 worker → last job waits ~83 minutes (fixed throughput, linear degradation). (2) *Extreme contention:* 100+ concurrent releases on one job → DynamoDB throttling possible, but the system remains correct (conditional writes never allow double-release). *Base case:* 10–50 users across 3 printers — handled comfortably (46 req/s, 0 errors at 100 users).

---

## 7. Impact

This project demonstrates core distributed systems concepts in a tangible, deployed system: message-driven processing (SQS), optimistic concurrency (DynamoDB conditional writes), at-least-once delivery with idempotency, and queue-based fault tolerance (ECS restart + SQS redelivery).

The API runs behind a **public ALB endpoint** — classmates can upload jobs, release them to printers, and poll status using `curl`, no AWS credentials required. We welcome classmates to test the system.

The architecture generalizes to any queue-based processing system with fixed-capacity workers and exactly-once requirements: document conversion, video transcoding, order fulfillment. The full project — Terraform, application code, experiment scripts, and documentation — is open-source and reproducible via a single command: `make deploy-fresh`.
