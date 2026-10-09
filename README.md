# KairoKube

KairoKube is a research prototype for message based state reconstruction (MS2M) around Kubernetes container checkpointing. It contains a migration manager HTTP API, RabbitMQ producer and consumer, mock migration workflow, and a Kubelet checkpoint request provider.

> **Prototype limitation:** the mock workflow simulates orchestration and writes synthetic checkpoint data. The Kubelet provider can request an FCC checkpoint, but KairoKube does not restore that checkpoint in a target container or provide remote artifact distribution, source/target fencing, or durable migration history. A migration reported successful in mock mode is not a live process migration. Do not use this project to cut over production workloads.

## What works

| Feature | Status |
|---|---|
| Mock migration orchestration and API | Available for local demonstration |
| RabbitMQ producer and consumer | Publishes/consumes durable messages; consumer uses manual acknowledgement |
| Kubelet FCC checkpoint request | Implemented; requires a compatible Kubernetes/Kubelet and CRI setup |
| Container restore and remote checkpoint transfer | Unsupported |
| StatefulSet migration | Unsupported; startup rejects this mode |
| Migration history | In memory; lost when manager restarts |
| Dashboard | Not included |
| Benchmark | Mock scenario only; not a real cluster performance measurement |

For the design audit and detailed file/function inventory, see [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) and [docs/KairoKube_File_and_Function_Guide.md](docs/KairoKube_File_and_Function_Guide.md).

## Requirements

- Go **1.26.6 or newer** (the `go` directive in `go.mod` is 1.26.6).
- RabbitMQ reachable from the manager, producer, and consumer. Docker is a convenient option for a local broker.
- PowerShell examples below are for Windows. Use equivalent environment variable syntax for your shell if needed.

The repository includes a local development configuration in [.env](.env). It is ignored by Git and is not loaded automatically by Go. Load it in each PowerShell terminal where you start a service:

```powershell
Get-Content .env | Where-Object { $_ -and $_ -notmatch '^\s*#' } | ForEach-Object {
  $name, $value = $_ -split '=', 2
  Set-Item -Path "Env:$name" -Value $value
}
```

The file selects the explicit mock workflow and uses the local RabbitMQ credentials shown below. Change the values in `.env` if your broker uses different credentials or ports.

## Run locally (mock demonstration)

### 1. Start RabbitMQ

In a terminal with Docker installed, run:

```powershell
docker run -d --name kairokube-rabbitmq `
  -p 5672:5672 -p 15672:15672 `
  -e RABBITMQ_DEFAULT_USER=kairokube `
  -e RABBITMQ_DEFAULT_PASS=kairokube-dev `
  rabbitmq:3.13-management
```

The broker AMQP URL for processes running on your host is:

```text
amqp://kairokube:kairokube-dev@localhost:5672/
```

The management UI is available at `http://localhost:15672` with the same credentials. The app declares/uses `microservices-queue` by default.

### 2. Start the migration manager

Open another PowerShell terminal at the repository root. Load the local mock settings and start the API:

```powershell
Get-Content .env | Where-Object { $_ -and $_ -notmatch '^\s*#' } | ForEach-Object {
  $name, $value = $_ -split '=', 2
  Set-Item -Path "Env:$name" -Value $value
}
go run ./cmd/migration-manager
```

The manager listens on port `8080`. Keep this terminal running. It still requires RabbitMQ even if you only want to use the migration API.

### 3. (Optional) Start the consumer and producer

In separate terminals, load `.env` before starting each service:

```powershell
Get-Content .env | Where-Object { $_ -and $_ -notmatch '^\s*#' } | ForEach-Object {
  $name, $value = $_ -split '=', 2
  Set-Item -Path "Env:$name" -Value $value
}
go run ./cmd/consumer
```

```powershell
Get-Content .env | Where-Object { $_ -and $_ -notmatch '^\s*#' } | ForEach-Object {
  $name, $value = $_ -split '=', 2
  Set-Item -Path "Env:$name" -Value $value
}
go run ./cmd/producer
```

The consumer exposes `/health` and `/state` on port `8081` by default. Set `STATUS_PORT` to change that port and `PROCESSING_DELAY_MS` to simulate per-message processing delay. The producer defaults to 1 message/second and continues until stopped; `MAX_MESSAGES` makes it exit after a fixed number of publishes (0 means unlimited).

### 4. Call the migration API

Start a simulated migration. In mock mode, names are demonstration inputs; `target_node` does not move a real Pod.

```powershell
$body = @{
  source_pod = "demo-consumer"
  namespace = "default"
  target_node = "demo-target"
} | ConvertTo-Json

$migration = Invoke-RestMethod `
  -Method Post `
  -Uri http://localhost:8080/migrations `
  -ContentType "application/json" `
  -Body $body

$migration
Invoke-RestMethod "http://localhost:8080/migrations/$($migration.migration_id)"
```

Useful endpoints:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | Manager health |
| `POST` | `/migrations` | Start a migration; requires `source_pod` and `target_node`; `namespace` defaults to `default`; returns `202` and a migration ID |
| `GET` | `/migrations` | List in-memory migration snapshots |
| `GET` | `/migrations/{id}` | Get one snapshot; returns `404` if not found |
| `GET` | `/metrics` | Prometheus text metrics |

### Stop the local broker

```powershell
docker rm -f kairokube-rabbitmq
```

## Build and verify

From the repository root:

```powershell
go test ./...
go vet ./...
go build ./...
```

To format Go source:

```powershell
gofmt -w ./cmd ./pkg ./scripts
```

The mock benchmark can be run with `./scripts/run_benchmark.ps1`; it writes benchmark result files in the repository root. Treat its results as synthetic scenario output, not cluster throughput.

## Configuration

The manager configuration is read from environment variables in `pkg/config/config.go`.

| Variable | Default | Notes |
|---|---|---|
| `RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | Required broker connection for manager. Producer/consumer also read this variable; their default is `amqp://guest:guest@rabbitmq:5672/` for container networking. |
| `QUEUE_NAME` | `microservices-queue` | Queue shared by manager, producer, and consumer. |
| `MIGRATION_MODE` | `mock` | `mock` simulates orchestration. `pod` requires Kubernetes credentials and a non-mock checkpoint provider. `statefulset` is rejected. |
| `CHECKPOINT_PROVIDER` | `mock` | `mock` or `kubelet`. Mock is only valid with mock migration mode. Kubelet checkpoint creation does not imply restore support. |
| `TRANSFER_PROVIDER` | `mock` | `mock` or `file`. The file provider copies on a local/shared filesystem; it is not a remote transfer service. |
| `MIGRATION_FEASIBILITY_POLICY` | `warn` | `warn`, `reject`, or `force` feasibility behavior. |
| `SERVER_PORT` | `8080` | Manager HTTP API port. |
| `MAX_REPLAY_TIME` | `5s` | Replay model input. Go duration format. |
| `MIN_CUTOFF_TIME` / `MAX_CUTOFF_TIME` | `1s` / `30s` | Cutoff bounds. Go duration format. |
| `METRICS_WINDOW` | `5s` | Rate measurement window. |
| `CHECKPOINT_TIMEOUT` | `30s` | Checkpoint phase timeout. |
| `TRANSFER_TIMEOUT` | `60s` | Transfer phase timeout. |
| `RESTORE_TIMEOUT` | `60s` | Restore phase timeout; actual container restore remains unsupported. |
| `REPLAY_TIMEOUT` | `30s` | Replay phase timeout. |
| `MIGRATION_TIMEOUT` | `5m` | Overall migration timeout. |
| `KUBELET_PORT` / `KUBELET_SCHEME` | `10250` / `https` | Kubelet connection settings. Kubelet provider uses API-server node proxy mode. |
| `CHECKPOINT_DIR` | OS temp dir + `kairokube-checkpoints` | Mock output and local file transfer directory. |
| `DEBUG_LOGGING` | `false` | Parsed boolean configuration. |

Producer-specific variables: `PUBLISH_RATE_MSG_PER_SEC` (default `1`), `MAX_MESSAGES` (default `0`, unlimited). Consumer-specific variables: `STATUS_PORT` (default `8081`) and `PROCESSING_DELAY_MS` (default `0`).

Invalid mode/provider combinations fail validation at manager startup. Durations use Go format such as `5s` or `2m`.

## Run in Kubernetes (mock mode)

The `k8s/` manifests are a development demonstration. They expect locally available images named `kairokube-manager:latest`, `kairokube-producer:latest`, and `kairokube-consumer:latest`; their pull policy is `Never`. Build and load those images into your cluster runtime before applying the workloads. RabbitMQ is a single non-persistent development fixture.

Create the credential Secret (choose a non-default password):

```powershell
$env:RABBITMQ_USER = "kairokube"
$env:RABBITMQ_PASSWORD = "replace-with-a-local-dev-password"
$env:RABBITMQ_URL = "amqp://$($env:RABBITMQ_USER):$($env:RABBITMQ_PASSWORD)@rabbitmq:5672/"
kubectl create secret generic rabbitmq-credentials `
  --from-literal=url="$env:RABBITMQ_URL" `
  --from-literal=username="$env:RABBITMQ_USER" `
  --from-literal=password="$env:RABBITMQ_PASSWORD"
```

Then apply resources in dependency order:

```powershell
kubectl apply -f k8s/rbac.yaml
kubectl apply -f k8s/rabbitmq.yaml
kubectl apply -f k8s/migration-manager.yaml
kubectl apply -f k8s/consumer-statefulset.yaml
kubectl apply -f k8s/producer.yaml
```

The manager manifest selects mock mode explicitly. To inspect it, use `kubectl get pods` and `kubectl logs deployment/migration-manager`. The shown API Service exposure depends on your cluster setup; port-forward the manager Pod or add an appropriate Service before making requests from outside the cluster. The manifests do not make live container restore available.

## Real checkpointing limitations

`MIGRATION_MODE=pod` with `CHECKPOINT_PROVIDER=kubelet` requires a configured Kubernetes client, appropriate API/Kubelet authorization, compatible CRI checkpoint support, and access to the node-local checkpoint archive. The project cannot currently restore the archive into a target runtime or safely hand off controller-owned workloads/PVCs. Do not point this mode at workloads expecting an end-to-end migration.
