# KairoKube: Optimizing Stateful Microservice Migration in Kubernetes with MS2M and Forensic Checkpointing

An implementation of the **Message-based Stateful Microservice Migration (MS2M)** framework with Kubernetes **Forensic Container Checkpointing (FCC)** based on CRIU and Kubelet APIs.

---

## 1. Architecture Overview

```text
                      +-------------------+
                      |     Producer      |
                      |  (Monotonic Seq)  |
                      +---------+---------+
                                |
                                v (AMQP Persistent Messages)
                      +---------+---------+
                      |     RabbitMQ      |
                      | (Durable Queues)  |
                      +---+-----------+---+
                          |           |
       (Source Messages)  |           | (Replayed / Post-Cutoff Messages)
                          v           v
            +---------------+       +---------------+
            |  Source Pod   |       |  Target Pod   |
            |  consumer-0   |       |  (worker-2)   |
            +-------+-------+       +-------^-------+
                    |                       |
                    | (Checkpoint Artifact) |
                    +--------> Transfer ----+
                                |
                    +-----------+-----------+
                    |   Migration Manager   |
                    |  (Adaptive Cutoff &   |
                    |    State Machine)     |
                    +-----------+-----------+
                                |
                                v
                    +-----------------------+
                    | Kubernetes & Kubelet  |
                    |  FCC API (/checkpoint)|
                    +-----------------------+
```

### Components
1. **Producer (`cmd/producer`)**: Emits structured JSON messages with monotonic sequence numbers, timestamps, and unique UUIDs at configurable rates (lambda).
2. **RabbitMQ Broker**: Handles durable queues, persistent deliveries, and manual ACKs.
3. **Consumer (`cmd/consumer`)**: Stateful microservice with in-memory state (`MessageCount`, `LastSequence`), manual ACKs, duplicate detection, and configurable processing delay (mu).
4. **Migration Manager (`cmd/migration-manager`)**: Coordinates the 15-step MS2M lifecycle, adaptive cutoff controller, rate monitoring (lambda, mu), feasibility checks, target restoration, and automated rollback.

---

## 2. Migration State Machine Lifecycle

```text
[IDLE]
  |
  v
[PREPARING] --------> (Feasibility Check & Node Validation)
  |
  v
[CHECKPOINTING] ----> (Kubelet FCC Request)
  |
  v
[CHECKPOINT_CREATED]
  |
  v
[TRANSFERRING] -----> (Artifact Copy & Checksum Verification)
  |
  v
[RESTORING] --------> (Target Scheduling & Readiness Verification)
  |
  v
[REPLAYING] --------> (Adaptive Cutoff Window: T_cutoff <= T_replay_max * (mu / lambda))
  |
  v
[CUTOFF] -----------> (Source Stop & Sequence Capture)
  |
  v
[FINALIZING] -------> (Queue Catch-Up & State Synchronization)
  |
  v
[COMPLETED]
```

### Failure & Rollback Path:
If an operational failure occurs at any stage (checkpoint error, transfer mismatch, restore timeout, readiness check failure):

```text
[FAILED_STAGE] ---> [ROLLING_BACK] ---> [SOURCE_RESTORED] ---> [FAILED]
```

Queued RabbitMQ messages remain preserved with at-least-once delivery semantics.

---

## 3. Adaptive Cutoff & Queuing Theory Model

The MS2M adaptive cutoff threshold `T_cutoff` is dynamically computed from real-time measured message arrival rate `lambda` and target processing capacity `mu`:

```text
N_accum = lambda * T_accum

T_replay = (lambda * T_accum) / mu_target <= T_replay_max

T_cutoff <= T_replay_max * (mu_target / lambda)
```

### Feasibility Policies (`MIGRATION_FEASIBILITY_POLICY`):
- Evaluates utilization `rho = lambda / mu_target`.
- **FEASIBLE** (`rho < 0.80`): Safe to proceed.
- **HIGH_RISK** (`0.80 <= rho < 1.00`): Target near capacity, logged with warning.
- **INFEASIBLE** (`rho >= 1.00`): Target cannot drain accumulated messages in steady state. Handled via policy:
  - `reject`: Aborts migration immediately before checkpointing.
  - `warn`: Logs warning and proceeds with clamped cutoff.
  - `force`: Forces migration.

---

## 4. Configuration & Environment Variables

| Variable | Default | Description |
|---|---|---|
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | RabbitMQ connection string |
| `QUEUE_NAME` | `microservices-queue` | Durable queue name |
| `MIGRATION_MODE` | `pod` | Workload controller mode (`pod` or `statefulset`) |
| `MIGRATION_FEASIBILITY_POLICY`| `warn` | Feasibility check policy (`warn`, `reject`, `force`) |
| `CHECKPOINT_PROVIDER` | `kubelet` (or `mock`) | Checkpoint provider (`kubelet`, `mock`, `file`) |
| `TRANSFER_PROVIDER` | `file` (or `mock`) | Transfer mechanism (`file`, `mock`) |
| `MAX_REPLAY_TIME` | `5s` | Maximum allowable replay duration `T_replay_max` |
| `MIN_CUTOFF_TIME` | `1s` | Lower bound clamp on cutoff duration |
| `MAX_CUTOFF_TIME` | `30s` | Upper bound clamp on cutoff duration |
| `METRICS_WINDOW` | `5s` | Sliding window duration for rate calculation (`lambda`, `mu`) |
| `CHECKPOINT_TIMEOUT` | `30s` | Timeout for container checkpointing |
| `TRANSFER_TIMEOUT` | `60s` | Timeout for artifact transfer |
| `RESTORE_TIMEOUT` | `60s` | Timeout for target Pod readiness |
| `MIGRATION_TIMEOUT` | `5m` | Overall migration timeout |
| `SERVER_PORT` | `8080` | Migration Manager HTTP REST API port |
| `KUBELET_PORT` | `10250` | Kubelet port for FCC API requests |
| `CHECKPOINT_DIR` | `/var/lib/kubelet/checkpoints` | Host directory containing checkpoint archives |

---

## 5. Kubernetes & Kubelet FCC Requirements

To run live Forensic Container Checkpointing (FCC) in a Kubernetes cluster:

1. **Kubelet Feature Gate**:
   Enable the container checkpoint feature gate on the worker nodes:
   ```bash
   --feature-gates=ContainerCheckpoint=true
   ```

2. **Container Runtime with CRIU**:
   CRI-O (1.25+) or containerd with CRIU installed on each Kubernetes worker node (`apt install criu`).

3. **RBAC Permissions**:
   Apply `k8s/rbac.yaml` to authorize the Migration Manager service account for `nodes/proxy` and Pod operations:
   ```bash
   kubectl apply -f k8s/rbac.yaml
   ```

---

## 6. Building and Running

### Build Binaries Locally
```powershell
go build ./...
```

### Run Unit Tests
```powershell
go test -v ./...
```

### Run the Benchmark Suite (4, 8, 12, 16, 20 msg/s)
```powershell
.\scripts\run_benchmark.ps1
# or: go run scripts/benchmark.go
```

Outputs:
- Terminal comparison table
- `benchmark_results.csv`
- `benchmark_results.json`

---

## 7. Migration HTTP REST API

### 1. Initiate Migration
```http
POST /migrations
Content-Type: application/json

{
    "source_pod": "consumer-0",
    "namespace": "default",
    "target_node": "worker-2"
}
```

**Response (202 Accepted):**
```json
{
    "migration_id": "MIG-0001",
    "status": "PREPARING"
}
```

### 2. Query Migration Status
```http
GET /migrations/MIG-0001
```

**Response (200 OK):**
```json
{
    "migration_id": "MIG-0001",
    "source_pod": "consumer-0",
    "namespace": "default",
    "target_node": "worker-2",
    "target_pod": "consumer-0-target",
    "mode": "pod",
    "state": "COMPLETED",
    "created_at": "2026-10-09T05:52:00Z",
    "downtime": 500221700,
    "checkpoint_size_bytes": 104960,
    "arrival_rate_lambda": 16.0,
    "target_processing_rate_mu": 20.0,
    "utilization": 0.8,
    "calculated_cutoff": 6250000000
}
```

### 3. Health & Observability
- `GET /health`: Returns `{"status":"healthy"}`
- `GET /metrics`: Standard Prometheus metrics exposition containing all 17 telemetry counters and gauges.
