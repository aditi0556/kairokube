# KairoKube

KairoKube is a research prototype for **MS2M** (message-based stateful microservice migration) combined with **Kubernetes container checkpointing** (Forensic Container Checkpointing / CRIU) and **OCI registry-based artifact transfer** (ORAS).

It provides:
- A **Migration Manager** HTTP API that orchestrates the migration lifecycle (preflight → checkpoint → transfer → restore → replay → cutover → verify, with rollback).
- A **Control Plane Dashboard** (`/dashboard`) with live WebGL 3D cluster topology, phase timeline waterfall, and real-time telemetry.
- An **OCI Registry Transfer Provider** powered by ORAS for pushing and pulling checkpoint layers to container registries (such as `ds0uz5/kairokube`).
- A **Kubernetes Kubelet FCC** provider that triggers container checkpoints on cluster nodes via the Kubelet API.
- A **Reference Workload Consumer** and **Producer** backed by RabbitMQ with cryptographic state tracking, duplicate detection, and replay synchronization.

---

## Status

| Capability | Status |
|---|---|
| Pod Live Migration (Standalone Pods) | Implemented (`MIGRATION_MODE=pod`); clones workload spec onto target node |
| Kubelet Container Checkpointing (FCC) | Checkpoint request and runtime preflight implemented; restore requires a compatible runtime provider |
| OCI Registry Artifact Transfer (ORAS) | Working (`TRANSFER_PROVIDER=registry`); verified push/pull to `ds0uz5/kairokube` |
| Local File Transfer | Working (`TRANSFER_PROVIDER=file`); verifies SHA-256 checksums |
| RabbitMQ Messaging Pipeline | Working (durable queues, manual ACK, duplicate detection) |
| WebGL 3D Control Plane Dashboard | Built-in at `http://localhost:8080/dashboard` with real-time SSE/WebSocket telemetry |
| StatefulSet Migration | Unsupported (`MIGRATION_MODE=statefulset` is rejected at validation) |

---

## Repository Layout

```
cmd/
  migration-manager/   Manager entry point: HTTP API, dashboard server, and migration-request consumer
  consumer/            Generic stateful consumer with cryptographic hashing & /state endpoint
  producer/            Publishes application messages and optional migration trigger requests
pkg/
  checkpoint/          Checkpoint & restore providers (Kubelet FCC, Podman, Ctr, Mock)
  config/              Centralized configuration management and validation
  k8s/                 Kubernetes Client-Go wrapper
  migration/           Manager, workflow state machine, 3D dashboard, metrics, HTTP API
  ms2m/                Adaptive cutoff and feasibility calculations
  rabbitmq/            AMQP client, message envelope schema, duplicate detection
  transfer/            Artifact transfer implementations (OCI Registry via ORAS, Local File)
build/                 Dockerfiles for manager, consumer, and producer
k8s/                   Kubernetes manifests, RBAC, and kind cluster definition
docker-compose.yml     Local multi-container stack: RabbitMQ, consumer, producer, migration-manager
```

---

## Prerequisites

- **Go 1.22+**
- **Docker** for building and pushing images. Kubernetes runtime support is listed below.
- **kubectl** & a cluster (e.g. kind) with `--feature-gates=ContainerCheckpoint=true` for live Kubernetes checkpointing
- **CRIU** (3.16+ or 4.2+) on cluster worker nodes

### Runtime support matrix

KairoKube separates image tooling from checkpoint runtime support:

| Environment | Dashboard/API | Real checkpoint migration |
|---|---:|---:|
| Docker Desktop + kind | Yes | No guarantee; use `EXECUTION_MODE=demo` or `mock` |
| Linux Kubernetes + CRI-O + CRIU | Yes | Supported checkpoint path; use a CRI-compatible restore provider |
| Linux Kubernetes + containerd + CRIU | Yes | Runtime/version dependent; validate before enabling migrations |
| Linux Docker Engine | Yes | Docker-native checkpoint experiments only |

Kubernetes checkpoints are created by Kubelet through the node's CRI runtime.
They must not be restored with Docker's native checkpoint command unless the
source artifact was created by Docker itself. Set `EXECUTION_MODE=full` only on
Linux nodes whose runtime and restore provider have been validated. The manager
now performs a runtime preflight and rejects unsupported Docker/Podman CRI
nodes before checkpointing.

For Fedora nodes, run `scripts/fedora_crio_prereq.sh` on every node, then use
`k8s/kubeadm-crio-config.yaml` with `kubeadm init` on the control plane and the
generated join command on workers. Enable CRI-O restore only on a patched CRI-O
release appropriate for the cluster.

Use `EXECUTION_MODE=demo` for a deployed dashboard with simulated migration, or
`EXECUTION_MODE=mock` for a fully local simulation. `auto` preserves the
explicit provider settings for production deployments.

---

## Quick Start (Docker Compose)

Start the local stack with RabbitMQ, the stateful consumer microservice, traffic producer, and the migration manager:

```bash
docker compose up --build
```

| Service | Port | Purpose |
|---|---|---|
| `migration-manager` | **8080** | Dashboard (`/dashboard`), API (`/migrations`), Prometheus (`/metrics`), `/health` |
| `consumer` | **8081** | Stateful workload; `GET /state` returns message counters and payload hashes |
| `rabbitmq` | **5672** / **15672** | Message broker & management console (`kairokube` / `kairokube-dev`) |
| `producer` | — | Continuously generates tasks and payloads to RabbitMQ |

Access the interactive dashboard in your browser:  
👉 **[http://localhost:8080/dashboard](http://localhost:8080/dashboard)**

---

## OCI Registry Checkpoint Transfer (ORAS)

KairoKube supports storing and transferring checkpoint artifacts as standard OCI layers using [ORAS](https://oras.land/):

```
[Source Node] ──(Checkpoint)──> [Manager] ──(oras push)──> [Docker Hub / OCI Registry]
                                                                     │
[Target Node] <──(Restore)───── [Manager] <──(oras pull)─────────────┘
```

### Configuration:
In your `.env` or Kubernetes manifest, configure:
```env
TRANSFER_PROVIDER=registry
CHECKPOINT_REGISTRY_REPOSITORY=ds0uz5/kairokube
CHECKPOINT_REGISTRY_USERNAME=ds0uz5
CHECKPOINT_REGISTRY_PASSWORD=<your-dockerhub-access-token>
```

When configured, checkpoints are tagged as `ds0uz5/kairokube:<pod-name>-<sha256>`, pushed as `application/vnd.kairokube.checkpoint.layer.v1+tar` artifacts, and pulled on demand on the target node.

---

## Initiating a Migration

### Via Dashboard UI:
Visit `http://localhost:8080/dashboard`, enter the source pod (`consumer-0`), namespace (`default`), and target node (`worker-2`), and click **Initiate migration**.

### Via HTTP REST API:
```bash
curl -X POST http://localhost:8080/migrations \
  -H "Content-Type: application/json" \
  -d '{"source_pod":"consumer-0","namespace":"default","target_node":"worker-2"}'
```

Response:
```json
{
  "migration_id": "MIG-0001",
  "status": "PREPARING"
}
```

Check progress:
```bash
curl http://localhost:8080/migrations/MIG-0001
```

---

## Migration Lifecycle

```
IDLE ──> PREPARING ──> CHECKPOINTING ──> CHECKPOINT_CREATED ──> TRANSFERRING
     ──> RESTORING ──> REPLAYING ──> CUTOFF ──> FINALIZING ──> COMPLETED

Any State ──(on error)──> ROLLING_BACK ──> SOURCE_RESTORED ──> FAILED
```

1. **Preflight**: Validates arrival rate ($\lambda$), service rate ($\mu$), target node schedulability, and feasibility.
2. **Checkpoint**: Captures memory pages via Kubelet FCC or container runtime.
3. **Transfer**: Streams checkpoint archive locally or pushes OCI layer to `CHECKPOINT_REGISTRY_REPOSITORY`.
4. **Restore & Replay**: Restores container state on target node, replays queued inflight messages from RabbitMQ.
5. **Cutover & Verify**: Switches traffic, terminates source, and verifies target readiness.

---

## Kubernetes Deployment

Deploy all components into an active cluster:

1. Create credentials secrets:
   ```bash
   # RabbitMQ broker credentials
   kubectl create secret generic rabbitmq-credentials \
     --from-literal=url="amqp://kairokube:kairokube-dev@rabbitmq:5672/" \
     --from-literal=username="kairokube" \
     --from-literal=password="kairokube-dev"

   # OCI Registry credentials for ORAS checkpoint push/pull
   kubectl create secret generic checkpoint-registry-credentials \
     --from-literal=username="ds0uz5" \
     --from-literal=password="<your-dockerhub-access-token>"
   ```

2. Apply manifests:
   ```bash
   kubectl apply -f k8s/rbac.yaml
   kubectl apply -f k8s/rabbitmq.yaml
   kubectl apply -f k8s/consumer-statefulset.yaml
   kubectl apply -f k8s/migration-manager.yaml
   kubectl apply -f k8s/producer.yaml
   ```

3. Access the manager:
   ```bash
   kubectl port-forward svc/migration-manager 8080:8080
   ```

---

## Configuration Reference

| Environment Variable | Default | Description |
|---|---|---|
| `MIGRATION_MODE` | `pod` | Workload controller mode: `pod` or `mock`. |
| `CHECKPOINT_PROVIDER` | `kubelet` | `kubelet` (Kubernetes FCC) or `mock`. |
| `TRANSFER_PROVIDER` | `registry` | `registry` (generic OCI artifact), `crio-registry` (annotated CRI-O image), `file`, or `mock`. |
| `EXECUTION_MODE` | `auto` | `auto`, `full`, `demo`, or `mock`; demo/mock select safe simulation providers. |
| `RESTORE_PROVIDER` | `docker` | `docker` for Docker-native checkpoints, `crio` for CRI-O checkpoint images, or `mock`. |
| `CHECKPOINT_REGISTRY_REPOSITORY` | `ds0uz5/kairokube` | Target OCI repository for checkpoint artifacts. |
| `CHECKPOINT_REGISTRY_USERNAME` | `ds0uz5` | Registry authentication username. |
| `CHECKPOINT_REGISTRY_PASSWORD` | empty | Registry Personal Access Token or password. |
| `RABBITMQ_URL` | `amqp://...` | AMQP broker connection URL. |
| `QUEUE_NAME` | `microservices-queue` | Workload task queue name. |
| `MIGRATION_REQUEST_QUEUE` | `migration-requests` | Asynchronous migration request trigger queue. |
| `CONSUMER_STATUS_URL` | `http://.../state` | Workload `/state` URL used to monitor throughput and state parity. |
| `MAX_REPLAY_TIME` | `5s` | Maximum acceptable message replay duration ($T_{\text{replay\_max}}$). |
| `MIN_CUTOFF_TIME` / `MAX_CUTOFF_TIME` | `1s` / `30s` | Bounds for adaptive MS2M cutoff window. |
| `SERVER_PORT` | `8080` | Migration Manager HTTP REST API and dashboard port. |

---

## Testing & Verification

```bash
# Run unit tests across packages
go test ./pkg/config/... ./pkg/rabbitmq/... ./pkg/transfer/... ./pkg/ms2m/...

# Verify builds
go build ./cmd/migration-manager
go build ./cmd/consumer
go build ./cmd/producer
```
