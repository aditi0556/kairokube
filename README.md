# KairoKube

KairoKube is a research prototype for **MS2M** (message-based stateful microservice migration) combined with **Kubernetes container checkpointing**. It contains:

- a **Migration Manager** HTTP API that orchestrates a migration lifecycle (checkpoint → transfer → restore → replay → cutover → verify, with rollback),
- a **RabbitMQ** producer that publishes application messages and optional migration requests,
- a **2048 game workload** (`game2048`) that consumes move commands, applies them deterministically, and exposes its state for replay verification,
- a **consumer** service with the same state contract, kept for comparison, and
- checkpoint providers: a **mock** provider for local simulation, a **Kubelet FCC** provider that calls the real checkpoint API, and a **podman** restore helper.

> **Important limitations.** The default mock workflow simulates orchestration and writes synthetic archives. A migration that reports `COMPLETED` in mock mode does **not** move a running process. The Kubelet checkpoint call works, but KairoKube cannot yet restore a Kubelet-produced archive into a target container. Do not use this project to cut over production workloads.

## Status

| Capability | Status |
|---|---|
| Mock migration lifecycle and REST API | Working; verified end to end in Docker Compose |
| RabbitMQ producer and consumer (manual ACK, durable queues) | Working |
| 2048 game workload with deterministic replay | Working; game logic and tests verified |
| Kubelet checkpoint API call | Verified on a kind cluster (containerd 2.1.3) with CRIU 4.2 built from source |
| Podman checkpoint and restore (rootful) | Verified end to end (`scripts/podman_ckpt_smoke.sh`) |
| Restore of a Kubelet-produced archive | **Not implemented.** The archive is valid, but no restore path is verified for it |
| StatefulSet migration | **Unsupported.** Startup rejects `MIGRATION_MODE=statefulset` |
| Migration history | In memory; lost when the manager restarts |
| Dashboard and Grafana | Not included |
| Benchmarks | Mock simulation only; not a cluster measurement |

## Repository layout

```
cmd/
  migration-manager/   Manager entry point: HTTP API and migration-request consumer
  producer/            Publishes application messages and optional migration requests
  consumer/            Reference consumer with in-memory state and /state endpoint
  game2048/            2048 workload: consumes moves, applies them, exposes /state
pkg/
  config/              Environment configuration and validation
  migration/           Manager, state machine, workloads, rollback, metrics, HTTP API
  ms2m/                Adaptive cutoff and feasibility calculations
  rabbitmq/            AMQP client, message schema, duplicate detection
  checkpoint/          Checkpoint provider interface; mock, Kubelet FCC, podman restore
  transfer/            Checkpoint artifact transfer with SHA-256 verification
  game2048/            Deterministic 2048 board logic
  k8s/                 Kubernetes client construction
build/                 Dockerfiles for each service
k8s/                   Kubernetes manifests and the kind cluster definition
game/moves.jsonl       Move script used by the producer in Compose
scripts/               Benchmark runner, CRIU probe, podman checkpoint smoke test
docker-compose.yml     Local stack: RabbitMQ, game2048, producer, migration-manager
```


## Prerequisites

- **Go 1.26.6 or newer** (see the `go` directive in `go.mod`).
- **Docker** (Docker Desktop on Windows) for the Compose stack and a local RabbitMQ broker.
- **kubectl** and a cluster only if you deploy to Kubernetes or run the Kubelet checkpoint path.
- **Linux with CRIU and root** only for real checkpoint and restore work. Rootless podman cannot checkpoint containers.

Examples use PowerShell. Use the equivalent syntax on other shells.

## Quick start (Docker Compose)

```powershell
docker compose up --build
```

| Service | Port | Purpose |
|---|---|---|
| `rabbitmq` | 5672 (AMQP), 15672 (management UI) | Message broker; user `kairokube` / `kairokube-dev` |
| `game2048` | 8081 | 2048 workload; `GET /state` returns the board and counters |
| `migration-manager` | 8080 | Migration API: `GET /health`, `POST /migrations`, `GET /metrics` |
| `producer` | none | Publishes the moves in `game/moves.jsonl` at 2 msg/s |

Check the stack:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8081/state
```

Stop it with `docker compose down`. Add `-v` only when you intend to delete the RabbitMQ volume.

## Start a mock migration

```powershell
$body = @{ source_pod = "game2048-0"; namespace = "default"; target_node = "worker-2" } | ConvertTo-Json
$m = Invoke-RestMethod -Method Post -Uri http://localhost:8080/migrations -ContentType application/json -Body $body
Invoke-RestMethod "http://localhost:8080/migrations/$($m.migration_id)"
```

The migration runs in the background. In mock mode it waits for the adaptive cutoff (about 30 s when traffic is low) before reaching `COMPLETED`. That delay is expected, not a hang. A second request for the same source pod while one is active returns `409 Conflict`.

## Run the services directly with Go

1. Start RabbitMQ:

   ```powershell
   docker run -d --name kairokube-rabbitmq -p 5672:5672 -p 15672:15672 `
     -e RABBITMQ_DEFAULT_USER=kairokube -e RABBITMQ_DEFAULT_PASS=kairokube-dev `
     rabbitmq:3.13-management
   ```

2. Load the local settings from `.env` in each terminal, then start a service:

   ```powershell
   Get-Content .env | Where-Object { $_ -and $_ -notmatch '^\s*#' } | ForEach-Object {
     $name, $value = $_ -split '=', 2
     Set-Item -Path "Env:$name" -Value $value
   }
   go run ./cmd/migration-manager
   ```

   `.env` is ignored by Git and is not loaded automatically. Start `go run ./cmd/game2048` and `go run ./cmd/producer` in separate terminals the same way.

## Configuration

Settings come from environment variables and are validated at startup. Defaults match `pkg/config/config.go`.

### Manager

| Variable | Default | Meaning |
|---|---|---|
| `RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | AMQP URL. Required. |
| `QUEUE_NAME` | `microservices-queue` | Application-message queue. |
| `MIGRATION_REQUEST_QUEUE` | `migration-requests` | Control queue for migration requests. |
| `CONSUMER_STATUS_URL` | empty | Workload `/state` URL used to measure processing rate. Compose sets `http://game2048:8081/state`. |
| `MIGRATION_MODE` | `mock` | `mock` or `pod`. `statefulset` is rejected. |
| `CHECKPOINT_PROVIDER` | `mock` | `mock` or `kubelet`. Mock mode requires `mock`; pod mode requires `kubelet`. |
| `TRANSFER_PROVIDER` | `mock` | `mock` or `file` (copies between directories on a shared filesystem). |
| `CHECKPOINT_DIR` | OS temp dir + `kairokube-checkpoints` | Checkpoint output and transfer directory. |
| `MIGRATION_FEASIBILITY_POLICY` | `warn` | `warn`, `reject`, or `force`. |
| `MAX_REPLAY_TIME` | `5s` | Maximum acceptable replay time (`T_replay_max`). |
| `MIN_CUTOFF_TIME` / `MAX_CUTOFF_TIME` | `1s` / `30s` | Bounds for the adaptive cutoff. |
| `METRICS_WINDOW` | `5s` | Sliding window for arrival and processing rates. |
| `CHECKPOINT_TIMEOUT` | `30s` | Checkpoint phase timeout. |
| `TRANSFER_TIMEOUT` | `60s` | Transfer phase timeout. |
| `RESTORE_TIMEOUT` | `60s` | Target readiness timeout. |
| `REPLAY_TIMEOUT` | `30s` | Parsed and validated; not yet enforced as a separate phase limit. |
| `MIGRATION_TIMEOUT` | `5m` | Overall migration deadline. |
| `KUBELET_PORT` | `10250` | Kubelet port for the Kubelet provider. |
| `KUBELET_SCHEME` | `https` | Parsed; the Kubelet provider currently uses the API-server node proxy. |
| `SERVER_PORT` | `8080` | Manager HTTP port. |
| `DEBUG_LOGGING` | `false` | Parsed; does not currently change log output. |


### Producer

| Variable | Default | Meaning |
|---|---|---|
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | Broker URL. |
| `QUEUE_NAME` | `microservices-queue` | Destination queue. |
| `PUBLISH_RATE_MSG_PER_SEC` | `1` | Publish rate. Values ≤ 0 fall back to 1. |
| `MAX_MESSAGES` | `0` | Stop after this many messages; `0` is unlimited. |
| `MESSAGE_PAYLOAD` | none | Payload for every message. Required unless `MESSAGE_FILE` is set. |
| `MESSAGE_FILE` | none | JSONL file; one payload per line, cycled in order. |
| `MIGRATION_TRIGGER_AFTER_MESSAGES` | `0` | If > 0, publish one migration request after this many messages. |
| `MIGRATION_SOURCE_POD` / `MIGRATION_NAMESPACE` / `MIGRATION_TARGET_NODE` | none / `default` / none | Contents of the migration request. |
| `MIGRATION_REQUEST_QUEUE` | `migration-requests` | Control queue for the request. |

### Workloads (consumer and game2048)

| Variable | Default | Meaning |
|---|---|---|
| `RABBITMQ_URL`, `QUEUE_NAME` | as above | Broker and queue. |
| `STATUS_PORT` | `8081` | Port for `/health` and `/state`. |
| `PROCESSING_DELAY_MS` | `0` | Simulated per-message processing delay. |

## HTTP API (migration manager)

| Method | Path | Response |
|---|---|---|
| `GET` | `/health` | `200` `{"status":"healthy","service":"migration-manager"}` |
| `POST` | `/migrations` | `202` `{"migration_id":"MIG-0001","status":"PREPARING"}`; `400` for missing fields; `409` if the source pod already has an active migration |
| `GET` | `/migrations` | `200` array of migration snapshots |
| `GET` | `/migrations/{id}` | `200` snapshot; `404` if unknown |
| `GET` | `/metrics` | Prometheus text format |

`POST /migrations` body fields: `source_pod` (required), `target_node` (required), and `namespace` (defaults to `default`). Records are kept in memory and are lost on restart.

## Migration lifecycle

```
IDLE → PREPARING → CHECKPOINTING → CHECKPOINT_CREATED → TRANSFERRING → RESTORING
     → REPLAYING → CUTOFF → FINALIZING → COMPLETED

Any state → ROLLING_BACK → SOURCE_RESTORED → FAILED     (on error)
```

Transitions are enforced by `isValidTransition` in `pkg/migration/state.go`. `COMPLETED` and `FAILED` are terminal.

## Kubernetes

The manifests in `k8s/` are a development demonstration:

- `rbac.yaml`: a ServiceAccount, a namespaced Role for pods, and a ClusterRole for node-proxy checkpoint calls.
- `rabbitmq.yaml`, `migration-manager.yaml`, `producer.yaml`, `consumer-statefulset.yaml`: workloads. They expect the images `kairokube-manager:latest`, `kairokube-producer:latest`, and `kairokube-consumer:latest` to be loaded into the cluster (`imagePullPolicy: Never`). Compose runs `game2048`; the StatefulSet manifest still runs the consumer.
- `kind-checkpoint.yaml`: the three-node kind cluster used for the Kubelet checkpoint verification.

Create the broker secret first (choose your own password; do not commit it):

```powershell
kubectl create secret generic rabbitmq-credentials `
  --from-literal=url="amqp://kairokube:<password>@rabbitmq:5672/" `
  --from-literal=username="kairokube" `
  --from-literal=password="<password>"
kubectl apply -f k8s/rbac.yaml
kubectl apply -f k8s/rabbitmq.yaml
kubectl apply -f k8s/migration-manager.yaml
kubectl apply -f k8s/producer.yaml
```

The manager Service is `ClusterIP`. Reach it with `kubectl port-forward svc/migration-manager 8080:8080`.


## Kubelet checkpointing

The Kubelet provider calls the documented checkpoint endpoint on a node:

```
POST /api/v1/nodes/<node>/proxy/checkpoint/<namespace>/<pod>/<container>
```

It needs the `ContainerCheckpoint` feature gate (Beta and enabled by default since Kubernetes 1.30), a container runtime that supports checkpointing, and CRIU 3.16 or newer on the node. A successful call returns `{"items":["<archive path>"]}`, and the archive is written on the node.

Verified on this project: a kind cluster (Kubernetes 1.34.0, containerd 2.1.3) with CRIU 4.2 built from upstream source inside each node returned a 328 KB archive containing CRIU image files. With the Debian CRIU 3.17.1 package on the same setup, the call failed with `vdso: Unexpected rt vDSO area bounds` on the WSL2 kernel.

**Restore of that archive is not implemented.** `KubeletFCCProvider.RestoreCheckpoint` returns an explicit error by design, so no migration can report a restored target through this path. `PodmanRestorer` in `pkg/checkpoint` restores podman-format archives; it was verified end to end but does not read Kubelet archives.

## Testing and validation

```powershell
go vet ./...
go test ./... -count=1
go build ./...
```

Last recorded results: `go vet` is clean, and `pkg/checkpoint`, `pkg/config`, `pkg/game2048`, `pkg/ms2m`, `pkg/rabbitmq`, and `pkg/transfer` pass. `pkg/migration` compiles, but on the development machine Windows Application Control blocked its test binary, so its results are not current. Re-run it on a machine that allows the test binary.

## Benchmarks

`scripts/run_benchmark.ps1` runs `scripts/benchmark.go`, a **mock simulation** over lambda values from 4 to 20 msg/s. It writes `benchmark_results.csv` and `benchmark_results.json` to the repository root. These results describe the simulated scenario and are not cluster performance measurements. Do not cite them as Kubernetes migration results.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Manager exits with `RabbitMQ is required` | Broker not running, or `RABBITMQ_URL` is wrong. |
| `MIGRATION_MODE=pod cannot use CHECKPOINT_PROVIDER=mock` | Pod mode needs `CHECKPOINT_PROVIDER=kubelet`. Or switch to mock mode. |
| `MIGRATION_MODE=statefulset is unsupported` | Expected. StatefulSet migration is not implemented. |
| `POST /migrations` returns `409` | A migration for that source pod is still active. Wait, or use another pod name. |
| Migration stays in `REPLAYING` for about 30 s | Expected in mock mode with little traffic; the cutoff waits for the calculated interval. |
| Kubelet call fails with `criu binary not found or too old` | Install CRIU 3.16 or newer on the node and put it on the node's `PATH`. |
| Kubelet call fails with `vdso: Unexpected rt vDSO area bounds` | CRIU/kernel incompatibility seen on WSL2 with CRIU 3.17.1. Use a newer CRIU. |
| `podman container checkpoint` fails with "requires root" | Run podman as root; rootless podman cannot checkpoint. |
| `pkg/migration` tests fail with "Application Control policy has blocked this file" | Local Windows policy blocks the test binary. Allow it, or run the tests elsewhere. |

## Known limitations

- Mock mode does not move a running process, and restore of Kubelet archives is not implemented.
- StatefulSet migration is unsupported; the existing code path is a stub.
- Migration history, deduplication state, and metrics are held in memory.
- Duplicate detection is an in-memory cache and does not give exactly-once processing across restarts.
- The HTTP API has no authentication. Keep it on a private network.
- No dashboard, Grafana configuration, or failure-injection controls are included.
- Benchmark output is synthetic.

## Documentation

`docs/` does not exist yet. The planned audit (`docs/IMPLEMENTATION_PLAN.md`) and file and function guide (`docs/KairoKube_File_and_Function_Guide.md`) have not been written. `scripts/generate_guide.py` is present but has not been run against the current code.

## License

No license file is included. Add one before distributing the project.

