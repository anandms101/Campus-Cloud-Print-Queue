# Campus Cloud Print Queue: Final Project

**Team:** Anand Mohan Singh, Vaibhav Thalanki, Pranav Viswanathan

**TL;DR:** Release-at-device cloud printing on AWS (Go Gin API + Python workers, ECS Fargate, DynamoDB, S3, per-printer SQS). Four experiments: Locust load (1,484 req, 0.0% errors, p99 370 ms), DynamoDB contention (50 concurrent releases, 1 winner, 0 errors), printer saturation, fault injection (kill task, 0 duplicates after recovery). The three closest projects in the class are [@1407](https://piazza.com/class/mk3hftotl6e229/post/1407) (Concert Tickets, same AWS stack, same one-winner contention story), [@1408](https://piazza.com/class/mk3hftotl6e229/post/1408) (Raft KV with Chaos Engineering, same fault-tolerance experiment template), and [@1402](https://piazza.com/class/mk3hftotl6e229/post/1402) (GatherYourDeals, same Locust methodology and saturation framing).

---

## Demo video

[![Watch the Campus Cloud Print Queue demo on YouTube](https://img.youtube.com/vi/W4ehZsmXToc/maxresdefault.jpg)](https://youtu.be/W4ehZsmXToc)

*Click the thumbnail to watch on YouTube: https://youtu.be/W4ehZsmXToc*

## Repo, report, and architecture

| Resource | Link |
|----------|------|
| GitHub repository | https://github.com/anandms101/Campus-Cloud-Print-Queue |
| Experiments report (PDF) | [`docs/CampusPrint_Final_Report.pdf`](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/docs/CampusPrint_Final_Report.pdf) |
| Architecture doc | [`docs/ARCHITECTURE.md`](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/docs/ARCHITECTURE.md) |

---

## What we built

The campus printer is always jammed, always queued, and always somehow printing someone else's resume. We built a release-at-device printing system that decouples submission from execution: students upload from anywhere, the job sits in `HELD` indefinitely, and the worker only prints once the student is physically at a chosen device.

| Layer | Choice | Why |
|-------|--------|-----|
| Compute | ECS Fargate (2 API tasks, 3 printer workers) | Serverless, pay-per-second, no instances to babysit |
| API | Go Gin | Goroutine concurrency + bulkhead, circuit breakers, rate limiting |
| State | DynamoDB on-demand | Conditional expressions give us optimistic concurrency without a lock service |
| Files | S3 | Durable, lifecycle-expired |
| Queues | SQS Standard, one per printer + DLQ | Isolates a stuck printer from the others |
| Edge | ALB | Layer-7 routing, health checks |
| IaC | Terraform (9 modules) | One-command deploy and teardown |

![System architecture](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/diagrams/system-overview.png?raw=true)

*Four-layer view: clients hit the ALB, two API tasks fan out to DynamoDB / S3 / per-printer SQS, and three printer workers each consume one queue.*

---

## Experiments at a glance

| # | What we tested | Headline result |
|---|----------------|-----------------|
| 1 | Locust load, 50 users, 60s | **1,484 req, 0.0% failure**, 25.1 req/s, p50 110 ms, p95 240 ms, p99 370 ms |
| 2 | 50 concurrent releases on one job | **1 success, 49 conditional-check failures, 0 server errors** |
| 3 | 50 jobs flooded into one printer | Queue depth peaks ~12, drains linearly. Other two queues stay empty |
| 4 | Kill printer task mid-job | **All 10 jobs reach `DONE`, zero duplicates** after ~60 to 95 s SQS recovery |

### Load test evidence

![Locust summary](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/diagrams/locust-summary.png?raw=true)

*Locust summary panel: 1,484 requests at 0% failure across the 60-second steady-state window.*

![Locust RPS over time](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/diagrams/locust-rps-over-time.png?raw=true)

*Throughput over time. Cold-start spike at ~45 req/s as users seed jobs, then a clean ~22 to 24 req/s plateau with no decay.*

![Per-endpoint latency](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/diagrams/locust-endpoint-latency.png?raw=true)

*Per-endpoint p50/p95/p99. `POST /jobs` is the heaviest path because of the S3 streaming upload; reads stay flat under 200 ms.*

![CloudWatch dashboard during load test](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/diagrams/cloudwatch-dashboard.png?raw=true)

*CloudWatch view of the same window. ECS API CPU peaks at ~4.5%, memory holds at ~12% of 512 MiB, and zero 5xx responses appear on the status-code panel.*

### Per-experiment terminal output

![Experiment 1 terminal output](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/experiment_results/exp1/Screenshot%202026-04-14%20at%2012.29.48%E2%80%AFPM.png?raw=true)

*Experiment 1: end-of-run Locust stats, 0% failure line.*

![Experiment 2 terminal output](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/experiment_results/exp2/Screenshot%202026-04-14%20at%2012.31.22%E2%80%AFPM.png?raw=true)

*Experiment 2: contention test showing the 1 / 49 / 0 split.*

![Experiment 3 terminal output](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/experiment_results/exp3/Screenshot%202026-04-14%20at%2012.47.39%E2%80%AFPM.png?raw=true)

*Experiment 3: 50 jobs released to one printer, all converging to `DONE`.*

![Experiment 4 terminal output](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/experiment_results/exp4/Screenshot%202026-04-14%20at%2012.50.54%E2%80%AFPM.png?raw=true)

*Experiment 4: post-kill recovery, all 10 jobs reach `DONE`, no duplicates.*

![Fault recovery sequence](https://github.com/anandms101/Campus-Cloud-Print-Queue/blob/main/public/diagrams/fault-recovery.png?raw=true)

*Sequence diagram of how a killed mid-flight job recovers via SQS redelivery and the worker's idempotent guard.*

---

## The three projects most like ours

| # | Project | Post | Closest to us on |
|---|---------|------|------------------|
| 1 | Concert Ticket Booking Platform | [@1407](https://piazza.com/class/mk3hftotl6e229/post/1407) | Infrastructure stack and one-winner contention |
| 2 | Distributed KV Store with Raft and Chaos Engineering | [@1408](https://piazza.com/class/mk3hftotl6e229/post/1408) | Fault-tolerance experiment template |
| 3 | GatherYourDeals: Data Service | [@1402](https://piazza.com/class/mk3hftotl6e229/post/1402) | Locust methodology and saturation framing |

We picked these by reading every Final Project post currently in the folder and picking the three with the deepest overlap on either architecture (us and @1407 share an AWS stack), failure-mode design (us and @1408 both built "kill, recover, verify safety" experiments), or experimental rigor (us and @1402 both leaned on Locust and ran into the same "low latency hides errors" trap).

---

### 1. Concert Ticket Booking Platform ([@1407](https://piazza.com/class/mk3hftotl6e229/post/1407))

> A ticket-booking platform on the same AWS stack we used (ECS Fargate, Locust, a relational store) tackling the same fundamental problem: serialize concurrent requests against a shared finite resource. Theirs is a seat in a concert; ours is a slot at a printer.

| | Their project | Ours |
|---|---------------|------|
| Compute | ECS Fargate | ECS Fargate |
| Load tool | Locust | Locust |
| Concurrency mechanism | DB row locking + Redis distributed lock comparison | DynamoDB conditional expressions |
| Failure experiment | Kill node mid-transaction (DB-side) | Kill worker mid-job (consumer-side, SQS redelivery) |
| Closest matching experiment | Their oversell-prevention test | Our Experiment 2 (50 concurrent releases) |

**Similarities.** Same compute, same load-testing tool, and the same "many requests, one winner" experiment design. Their oversell-prevention test reads almost identically to our Experiment 2: fire many concurrent attempts at one row, verify exactly one succeeds. Both teams also include a fault-injection-style experiment in the final report.

**Differences.** They serialize through DB row locking and additionally compared a Redis distributed lock as a third strategy, which we did not try. We avoided a lock service entirely and let DynamoDB conditional writes handle ordering at single-digit-millisecond latency. Their Experiment 4 kills nodes mid-transaction (stateful DB recovery); ours kills the consumer worker and relies on SQS at-least-once redelivery plus idempotent state transitions. Different recovery story, same final question: did the system stay correct?

**What we learned.** Their Redis lock comparison is a clean control we should have included. We claim "no distributed locks needed" but only against ourselves. Pitting our DynamoDB conditional write against an explicit Redis SETNX lock would have made the trade-off (latency, operational cost, failure modes) much more concrete. We are adding this to our follow-on list.

---

### 2. Distributed Key-Value Store with Raft and Chaos Engineering ([@1408](https://piazza.com/class/mk3hftotl6e229/post/1408))

> A 5-node Raft KV store built with `hashicorp/raft` and BoltDB, with three rigorous chaos experiments: leader crash recovery, network partitions (minority isolation, leader isolation, symmetric split), and read scaling under different consistency modes.

| | Their project | Ours |
|---|---------------|------|
| Replication | `hashicorp/raft`, 5-node cluster | Leased to DynamoDB |
| Failure types tested | Leader crash, 3 partition topologies, back-to-back kills | Single worker kill |
| Recovery floor observed | ~1000 ms (election timeout) | ~60 s (SQS visibility timeout) |
| Safety check | Post-heal log mismatches across nodes | Job duplicates after redelivery |
| CAP behavior visible | Yes (chose CP, halted on symmetric split) | No (managed services hide it) |

**Similarities.** This is the project closest to ours in spirit on the fault-tolerance side. Both teams structured the experiment around "kill something while it is doing work, then verify correctness once it heals." Our Experiment 4 (kill a printer task mid-job, count duplicates after redelivery) and their leader-crash and back-to-back-kill experiments share the same template: induce the failure, time the recovery, then check post-heal state for any safety violation. Both reports also reach the same uncomfortable conclusion that recovery time is not knob-tunable in the way you would expect: their election timeouts hit a 1000 ms floor, and our recovery floor is the 60-second SQS visibility timeout.

**Differences.** They built consensus from scratch and own the replication state machine. We ducked that entirely by leasing replication to DynamoDB and ordering to SQS. They got to study CAP first-hand (their symmetric 2-2-1 split halted writes everywhere, choosing CP over availability). Our system has no concept of partitions because the AWS managed services hide them, so we can only inherit that behavior, not observe it.

**What we learned.** The depth of their fault matrix (three distinct partition topologies, three election timeouts, back-to-back kills) is a much higher bar than our single "kill one task" scenario. For a v2, we would add (a) two simultaneous worker kills to test concurrent recovery, and (b) an `iptables` block to simulate a worker that loses SQS connectivity but stays alive, which is a failure mode managed services do not protect us from.

---

### 3. GatherYourDeals: Data Service Final Report ([@1402](https://piazza.com/class/mk3hftotl6e229/post/1402))

> The data-service half of GatherYourDeals (a Postgres + Redis auth/token service on Railway) ran load tests in Locust across 8 different deployment configurations and reported login/refresh latency at p50 and p95 for each.

| | Their project | Ours |
|---|---------------|------|
| Load tool | Locust | Locust |
| Reporting style | p50/p95 per endpoint, per run | p50/p95/p99 per endpoint, single run |
| Number of runs | 8 (across deployment changes) | 1 baseline run |
| Bottleneck found | CPU + DB connection pool saturation | `POST /jobs` S3 upload latency |
| Methodology insight we share | Low latency can hide errors | Same: validation-rejected requests look "fast" |

**Similarities.** Same load-testing tool and same way of presenting results, percentile slices per endpoint with a callout for which run saturated which dependency. They also walked through the difference between "low latency because it is fast" and "low latency because the request short-circuited and did less work." That is exactly the trap we hit interpreting our Experiment 1 numbers: the 81 ms p50 on the bad-printer release looks better than the 100 ms valid release, but only because validation rejects it before the SQS call.

**Differences.** Their bottleneck is CPU and connection-pool saturation in a relational DB on a single Railway box. Ours is `POST /jobs` S3 upload latency on Fargate. Different failure mode, same shape on the chart. They also ran the same test repeatedly across deployment changes (SQLite local, Postgres on Railway, +Redis, transaction-pooler mode), which is something we did exactly once at one configuration.

**What we learned.** Their Run 6 vs Run 7 callout, where a Railway service outage produced unreproducible latency tails, was a useful reminder that we should report results from at least two separate runs and flag any outlier we cannot reproduce. Our 603 ms DELETE outlier in Experiment 1 is a candidate for exactly that kind of "Heisenberg failure" footnote.

---

## Closing

If anyone from those three teams (or anyone else with overlapping work) wants to compare notes, we are especially interested in:

1. Anyone who pitted DynamoDB conditional writes against an explicit Redis SETNX lock for the same workload. We would love to see the latency and cost numbers side by side.
2. How teams designed their second-order failure experiments: simultaneous kills, partial network failures, slow consumers. Our single-task-kill scenario is the obvious next thing to extend.
3. Anyone whose load-test runs surfaced reproducibility issues like the GatherYourDeals Railway outage, and how you reported them.

Comments and roasts welcome.
