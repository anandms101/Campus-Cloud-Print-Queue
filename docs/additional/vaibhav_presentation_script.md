

## 1. Job state machine 

States:
- **HELD** — job uploaded, sitting in the cloud, not assigned to any printer yet
- **RELEASED** — user has released it to a specific printer, message is on that printer's SQS queue
- **PROCESSING** — a printer worker has picked it up
- **DONE** — successfully printed (terminal)
- **CANCELLED** — user cancelled before release (terminal)
- **FAILED** — worker hit max retries (terminal)

![State Design](../public/diagrams/state-machine.png)

---

## 2. API routes 


| Endpoint | Purpose | State change |
|----------|---------|--------------|
| `GET /health` | ALB liveness, always 200 | — |
| `GET /health/ready` | probes DynamoDB, S3, SQS | — |
| `POST /jobs` | upload file → S3 + DynamoDB | **creates `HELD`** |
| `GET /jobs?userId=...` | list a user's jobs (via GSI) | — |
| `GET /jobs/:id` | fetch one job | — |
| `POST /jobs/:id/release` | release to a printer | **`HELD → RELEASED`** |
| `DELETE /jobs/:id` | cancel | **`HELD → CANCELLED`** |

---

## 3. Resilience architecture 

![State Design](../public/diagrams/api-resilience-pipeline.png)


1. **Rate limit** — global token bucket: 100 requests/sec sustained, burst 20. Anything above that gets HTTP 429 before we even touch a handler. Protects against runaway clients.

2. **Per-route timeout** — 60s for uploads, 30s for everything else. If a handler hangs on a slow AWS call, we return 504 instead of tying up the goroutine forever.

3. **Bulkhead (uploads only)** — a semaphore with capacity 4. Only 4 uploads can run concurrently. Request 5 gets rejected immediately with 429.

4. **Circuit breakers** — one per AWS dependency (S3, DynamoDB, SQS). If AWS starts failing, the breaker trips and we fail fast with 503 instead of hanging for 30 seconds on every request. I'll explain the state machine in a minute.

5. **Compensating action** — on `POST /jobs`, if S3 upload succeeds but DynamoDB write fails, I delete the orphan S3 object. No dangling files.

---

## 4. Code walkthrough

`services/api-gin/handlers.go:189` (bulkhead)

```go
uploadSemaphore: make(chan struct{}, 4)

// inside createJobHandler:
select {
case a.uploadSemaphore <- struct{}{}:
    defer func() { <-a.uploadSemaphore }()
default:
    c.JSON(429, gin.H{"detail": "too many active uploads..."})
    return
}
```
 `services/api-gin/handlers.go:425` (conditional write on release)

```go
UpdateExpression:    "SET #s = :new_status, printerName = :printer, ...",
ConditionExpression: "#s = :held",
```

`services/api-gin/handlers.go:481` (compensating rollback)

```go
if err := a.sendJobToPrinter(ctx, &updated); err != nil {
    // SQS failed after we already flipped DynamoDB to RELEASED
    rollbackRelease(ctx, jobID) // revert RELEASED → HELD
    ...
}
```


---

## 5. Circuit breakers


![State Design](../public/diagrams/circuit-breaker-state.png)


Walk through the three states:

- **CLOSED (normal)** — every request passes through to AWS. We count consecutive failures.
- **OPEN (tripped)** — triggered after **5 consecutive failures**. Now every request fails *instantly* with 503, without even trying AWS. This protects two things:
  1. The client — 20ms response instead of a 30s hang
  2. The downstream — we stop making things worse when it's already struggling
- **HALF-OPEN (testing)** — after a 15s cooldown, we let **3 probe requests** through. If all 3 succeed, AWS is healthy again → back to CLOSED. If any fail → back to OPEN for another 15s.

---

## 6. Tying it to the experiments 


> - **Experiment 1 (load test):** 5,492 requests at 100 concurrent users, **zero errors**. The middleware chain + bulkhead + breakers kept the service stable.
> - **Experiment 2 (contention):** 50 concurrent releases on one job, **exactly 1 success, 49 clean 409 conflicts, zero errors**. The conditional write + CCE filter are what make that work."
