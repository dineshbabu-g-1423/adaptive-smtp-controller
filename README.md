# Adaptive SMTP Traffic Controller

> A Go-based delivery **control plane** that decides **when, where, and how fast**
> each email should be delivered.

This is **not** another MTA simulator. It focuses on the hard scheduling /
control problem that lives *inside* a high-volume MTA:

- **Hierarchical throttling** — effective rate = `min()` across Global → Customer → Domain → Provider → IP
- **Queue-aware retry** — backoff scales with defer count, queue depth, and provider pressure (no retry storms)
- **Provider circuit breaker** — `HEALTHY → DEGRADED → THROTTLED → OPEN` for blast-radius containment
- **IP warmup engine** — daily ramp with automatic **pause / rollback** on bad signals
- **Reputation-aware routing** — per-`(IP, provider)` scores pick the best IP (not round-robin)
- **Delivery-outcome classification** — `success / deferred / bounce / timeout` from SMTP replies

The controller continuously answers: *Can I send this now? Which IP? Which queue?
Has this destination started throttling us? Retry now or back off? Is this IP
still warming up? Can I reduce traffic without stopping everything?*

---

## Architecture

```
                         REST API
                            │
                            ▼
                    ┌──────────────┐
                    │ Message Queue│  (time-ordered priority heap)
                    └──────┬───────┘
                           ▼
                 ┌───────────────────┐
                 │ Delivery Scheduler│  (the control loop)
                 └─────────┬─────────┘
             ┌─────────────┼─────────────┐
             ▼             ▼             ▼
        Customer       Provider        Domain
        Limiter        Limiter         Limiter      ── min() admission
             └─────────────┼─────────────┘
                           ▼
                    ┌──────────────┐
                    │ IP Pool      │  shared / dedicated
                    │ + Warmup     │  pause & rollback
                    └──────┬───────┘
                           ▼
                    ┌──────────────┐
                    │ Reputation   │  (IP, provider) scores
                    │ Router       │
                    └──────┬───────┘
                           ▼
                      SMTP Transport  ── Gmail / Outlook / Yahoo (simulated)
                           ▼
                    Outcome Classifier
                           ▼
              Success │ Deferred │ Bounce │ Timeout
                           ▼
                    Retry Scheduler (queue-aware backoff)
                           ▲
                    Circuit Breaker (per provider)
```

---

## Project layout

```
adaptive-smtp-controller/
├── cmd/
│   ├── controller/      # REST API + background scheduler
│   └── simulator/       # offline "killer demo" scenarios
├── internal/
│   ├── model/           # core domain types
│   ├── throttle/        # hierarchical token-bucket limiters + admission
│   ├── retry/           # queue-aware backoff policy
│   ├── circuitbreaker/  # per-provider blast-radius containment
│   ├── warmup/          # IP warmup schedules w/ pause & rollback
│   ├── routing/         # reputation-aware IP selection (shared/dedicated)
│   ├── reputation/      # EWMA per-(IP, provider) scoring
│   ├── smtp/            # transport interface + outcome classifier
│   ├── simulator/       # fake Gmail/Outlook/Yahoo providers
│   ├── scheduler/       # the control loop tying it all together
│   ├── config/          # YAML loaders
│   ├── idgen/           # dependency-free message IDs
│   └── api/             # HTTP handlers
├── configs/             # limits.yaml, providers.yaml, warmup.yaml
├── Dockerfile · docker-compose.yml · Makefile
└── .github/workflows/ci.yml
```

---

## Quick start

```bash
# run the control plane (REST API on :8080)
make run        # or: go run ./cmd/controller

# run the offline incident demo
go run ./cmd/simulator outlook-throttle
go run ./cmd/simulator outlook-recovery

# tests (race + coverage)
make test
```

---

## The killer demo

Start with all providers flowing at their ceilings:

```
gmail    100 msg/s     outlook  100 msg/s     yahoo  100 msg/s
```

Trigger an Outlook incident:

```
$ go run ./cmd/simulator outlook-throttle
```

The controller detects the `421 4.7.650 Too many messages` storm and **dynamically
reduces only the Outlook route** while Gmail and Yahoo keep running:

```
== OUTLOOK THROTTLING DETECTED ==
  provider rates (current/base):
    provider:outlook   40/100 msg/s     ← throttled down
    provider:gmail    100/100 msg/s     ← NORMAL
    provider:yahoo    100/100 msg/s     ← NORMAL
  circuit states:
    outlook  DEGRADED
    gmail    HEALTHY
    yahoo    HEALTHY

Blast radius: OUTLOOK only — Gmail & Yahoo unaffected.
```

Then recovery — the breaker closes and the rate ramps back toward base:

```
$ go run ./cmd/simulator outlook-recovery
== RECOVERY DETECTED ==
    provider:outlook   80/100 → climbing
    outlook  HEALTHY
```

---

## REST API

| Method | Path                                   | Description                              |
|--------|----------------------------------------|------------------------------------------|
| GET    | `/healthz`                             | liveness                                 |
| POST   | `/v1/messages`                         | enqueue one or many (`count`) messages   |
| GET    | `/v1/stats`                            | sent / deferred / bounced / throttled    |
| GET    | `/v1/rates`                            | current vs base rate per limiter         |
| GET    | `/v1/reputation`                       | per-`(IP, provider)` scores              |
| GET    | `/v1/breakers`                         | circuit state per provider               |
| GET    | `/v1/attempts`                         | recent delivery attempts (diagnostics)   |
| POST   | `/v1/simulate/{provider}/{action}`     | `throttle` / `recover` an incident       |

Example:

```bash
curl -X POST localhost:8080/v1/messages -d '{
  "customer_id":"acme","from":"news@acme.io","to":"u@outlook.com",
  "domain":"acme.io","provider":"outlook","count":5000
}'

curl -X POST localhost:8080/v1/simulate/outlook/throttle
curl localhost:8080/v1/rates
curl localhost:8080/v1/breakers
curl -X POST localhost:8080/v1/simulate/outlook/recover
```

---

## Configuration

- **`configs/limits.yaml`** — rate ceilings for every hierarchy level
- **`configs/providers.yaml`** — shared/dedicated IP pools + seeded reputation
- **`configs/warmup.yaml`** — per-IP daily ramp + pause/rollback thresholds

---

## Design notes

- **Atomic multi-level admission** — a message consumes a token at *every* level
  or none; partial grants are rolled back, so one exhausted level can't leak quota.
- **Dynamic rate control** — throttle signals multiplicatively cut the offending
  provider/IP rate; clean sends additively recover it toward base.
- **Backpressure, not retry storms** — delay grows with `defer_count × queue_pressure
  × provider_pressure`, and messages bounce once their retry budget is exhausted.
- **No heavyweight deps** — standard library + `gorilla/mux` for routing and
  `yaml.v3` for config; everything else is hand-rolled and unit-tested.
```
