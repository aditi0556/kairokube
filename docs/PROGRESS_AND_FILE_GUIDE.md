# KairoKube: Progress Report and File Guide

This document records what has been built so far, what works, what does not, and what each file in the repository does. It describes the code as it is on disk. Where a claim is not verified, it says so.

## 1. Project purpose

KairoKube is a research prototype of **MS2M** (message-based stateful microservice migration) on top of **Kubernetes container checkpointing**. The goal is to move a stateful service between nodes with minimal downtime: checkpoint the process, transfer the checkpoint, restore it on the target, then replay the messages that arrived during the move until the target catches up.

The project has four parts:

1. A **migration manager** that runs the migration state machine and exposes an HTTP API.
2. **Workloads** (a 2048 game and a plain consumer) that consume RabbitMQ messages and expose their state for verification.
3. A **producer** that publishes messages and can trigger migrations.
4. **Checkpoint providers**: a mock, a Kubelet checkpoint client, and a podman restore helper.

## 2. Status at a glance

| Area | Status | Evidence |
|---|---|---|
| Mock migration lifecycle and HTTP API | Works end to end | Compose run reached `COMPLETED` |
| RabbitMQ producer and consumer | Works | Consumer processed every message, 0 duplicates |
| 2048 workload (`game2048`) | Works | Game logic tests pass; live board updates |
| Duplicate detection (in memory) | Works, limited | Unit tests; not durable across restarts |
| Adaptive cutoff and feasibility | Implemented, unit tested | `pkg/ms2m` tests pass |
| Kubelet checkpoint API call | Works on kind | Returned a 328 KB CRIU archive |
| Podman checkpoint and restore (rootful) | Works | `scripts/podman_ckpt_smoke.sh` exit 0 |
| Restore of a Kubelet archive | **Not working** | No restore path verified yet |
| Live migration with real restore | **Not working** | Blocked on the two items above |
| StatefulSet migration | Not implemented | Startup rejects the mode |
| Dashboard, Grafana, failure injection | Not implemented | Not started |

## 3. Progress history

**Phase 1: audit and baseline.** The repository had a manager, producer, consumer, and mock workflow, and `go vet` and `go test` passed. The audit found a wrong Compose base image tag (`golang:1.26.8-alpine3.22`, which does not exist on Docker Hub), a producer manifest that exited without a payload, a transfer bug that could truncate a checkpoint when source and destination were the same file, and HTTP status codes that were always 500.

**Phase 2: fixes.** The transfer self-copy bug was fixed and covered by a regression test. The API returns 400 for bad input and 409 for a conflicting active migration. The Dockerfiles use `golang:1.26-alpine`, the producer manifest sets its payload, and the Compose stack builds and runs.

**Phase 3: the 2048 workload.** The consumer was replaced in Compose by `game2048`, which applies moves to a 4x4 board. A bug was fixed: the board started empty, so no tile ever spawned. `NewBoard` now seeds two tiles with a fixed seed, so every node starts from the same state.

**Phase 4: CRIU investigation.** Rootless podman cannot checkpoint, so the checkpoint path runs as root. Podman checkpoint and restore worked after installing `iptables` (CRIU's network dump calls `iptables-restore`) and granting CRIU its capability. A kind cluster was created with three nodes. The Kubelet checkpoint endpoint was reachable, but the dump failed on the stock Debian CRIU 3.17.1 with `vdso: Unexpected rt vDSO area bounds`. Building CRIU 4.2 from source inside the nodes fixed it, and the Kubelet call then returned a real archive.

**Phase 5: restore investigation (current).** containerd 2.1.3 exposes `ctr containers checkpoint --task` and `ctr containers restore --live`, confirmed from `--help` inside the node. A counter task runs under `ctr`. The checkpoint test failed because the rebuilt node had no CRIU. The CRIU 4.2 source build on the fresh node fails in `make` at `images/stats.pb-c.c`. The restore has not been tested.

**Phase 6: cleanup and documentation.** About 22 one-off diagnostic scripts were removed. Doc comments were added to production declarations across most packages. The README was rewritten. Some test binaries are blocked by Windows Application Control, a local policy that was not bypassed.


## 4. Repository tree

```
KairoKube/
├── cmd/                     Executable entry points
│   ├── migration-manager/   Manager: HTTP API and migration-request consumer
│   ├── producer/            Publishes application messages and migration requests
│   ├── consumer/            Reference consumer with /state endpoint
│   └── game2048/            2048 workload with /state endpoint
├── pkg/                     Library packages
│   ├── config/              Environment configuration and validation
│   ├── migration/           Manager, state machine, workloads, rollback, metrics, API
│   ├── ms2m/                Adaptive cutoff and feasibility math
│   ├── rabbitmq/            AMQP client, message schema, duplicate detection
│   ├── checkpoint/          Checkpoint provider interface and implementations
│   ├── transfer/            Checkpoint transfer with checksum verification
│   ├── game2048/            Deterministic 2048 board logic
│   └── k8s/                 Kubernetes client construction
├── build/                   Dockerfiles for each service
├── k8s/                     Kubernetes manifests and kind cluster definition
├── game/                    Move script used by the producer in Compose
├── scripts/                 Benchmark, CRIU and restore probes, smoke test
├── docs/                    This document
├── docker-compose.yml       Local stack: RabbitMQ, game2048, producer, manager
├── go.mod, go.sum           Module definition and pinned dependencies
├── .env                     Local mock-mode settings (Git-ignored)
├── .gitignore               Files excluded from Git
└── README.md                Overview, setup, configuration, API
```


## 5. File-by-file guide

Each entry gives the file's role, its main functions, what it depends on, and its side effects. Test files are listed with the production code they cover.

### 5.1 Entry points (`cmd/`)

**`cmd/migration-manager/main.go`** starts the manager. It loads and validates config (`config.Load`, `Validate`), creates the Kubernetes client (`k8s.NewClient`, optional in mock mode), selects the checkpoint provider (Kubelet or mock), the transfer provider (file or mock), and the workload controller (pod or mock), connects to RabbitMQ, starts the migration-request consumer, and then either runs one migration from `-source`/`-target` flags or serves the HTTP API until SIGINT/SIGTERM. Side effects: opens the RabbitMQ connection, listens on `SERVER_PORT`. Helper: `getEnv`.

**`cmd/producer/main.go`** publishes application messages at `PUBLISH_RATE_MSG_PER_SEC`. It reads payloads from `MESSAGE_PAYLOAD` or `MESSAGE_FILE` (`readPayloadFile`), wraps each in a `rabbitmq.Message` with a UUID and sequence number, and optionally publishes one migration request after `MIGRATION_TRIGGER_AFTER_MESSAGES`. Helpers: `getEnv`, `getFloatEnv`, `getIntEnv`. It refuses to start without a payload, so it never publishes synthetic data.

**`cmd/consumer/main.go`** is the reference consumer. It consumes with manual ACK, rejects invalid envelopes with NACK (no requeue), counts duplicates without applying them, and exposes `State` through `/health` and `/state`. Its default processing delay is 50 ms (`getProcessingDelay`).

**`cmd/game2048/main.go`** is the 2048 workload. It consumes move commands, decodes them with `game2048.DecodeMove`, rejects duplicates by message ID, applies each move with an RNG seeded from the message sequence, and ACKs only after applying. Its `/state` returns the board, counters, `game_over`, and `last_sequence`. Its default processing delay is 0.

### 5.2 Workloads and their message contract

**`pkg/game2048/board.go`** holds the pure game logic. `Board` stores a 4x4 grid, score, and move count. `Apply` slides and merges in a direction and spawns one tile only when the board changed. `spawn` picks an empty cell and a value (2 with probability 0.9) from the supplied RNG, so the same sequence produces the same board on any node. `RNGForSequence` derives that RNG. `DecodeMove` and `ParseDirection` validate move payloads of the form `{"move":"left"}`. `IsOver` reports when no move is possible.

**`pkg/game2048/board_test.go`** tests merging, no-op moves, determinism across two runs, the starting board, game-over detection, and move parsing.

**`game/moves.jsonl`** is the move script the producer cycles through in Compose.

**`pkg/rabbitmq/message.go`** defines the application message: `Version`, `ID` (UUID), `Sequence`, `Timestamp`, `Payload`. `NewMessage` and `Encode` build and serialize it; `DecodeMessage` validates it (ID required, version 0 or 1). `DuplicateDetector.CheckAndRecord` remembers IDs in memory up to a capacity and retention window, so duplicates are detected only while they remain in memory.

**`pkg/rabbitmq/client.go`** wraps the AMQP connection: `NewClient`, `Publish` (durable queue, persistent messages), `Consume` (manual ACK, QoS prefetch 1), `PublishMessage`, and `InspectQueue` (depth and consumer count, used by the rate monitor). Durable queues and persistent delivery mean messages survive a broker restart; they are not confirmed by publisher confirms, so a publish can still be lost in a broker failure window.

**`pkg/rabbitmq/migration_request.go`** defines the control message the manager consumes: `NewMigrationRequest`, `Encode`, and `DecodeMigrationRequest` (requires `id`, `source_pod`, `target_node`).

**`pkg/rabbitmq/message_test.go`** covers encode/decode, schema versions, and duplicate detection.

The migration-control schema lives in `migration_request.go` and is kept separate from application messages on purpose. It has no dedicated test file; it is exercised only through the manager's request consumer.



### 5.3 Migration manager (`pkg/migration/`)

**`manager.go`** holds `Manager`, the orchestrator. `NewManager` wires the config, providers, workload controller, rate monitor, rollback manager, and metrics. `CreateMigration` rejects a second active migration for the same source pod and assigns an ID (`MIG-0001`, ...). `StartMigrationAsync` moves the record to `PREPARING` and runs `executeWorkflow` in a goroutine, so the HTTP request returns immediately. `executeWorkflow` runs the steps in order: source and target validation (steps 1–2), feasibility and rate measurement (pre-flight), checkpoint (steps 4–5), transfer (step 6), restore (step 7), replay with adaptive cutoff (steps 8–9), cutoff and source stop (steps 10–11), finalize (steps 12–13), and completion (steps 14–15). Failures call `RollbackManager.ExecuteRollback` with the phase that failed. `StartMigrationRequestConsumer` consumes control messages from the migration-request queue and starts migrations for them, rejecting duplicate request IDs. `GetMigration`, `ListMigrations`, `GetRateMonitor`, and `GetMetrics` are read accessors.

**`state.go`** defines `MigrationState` and the `Migration` record (IDs, pods, timings, rates, cutoff, errors). `SetState` validates each transition with `isValidTransition` and logs it. `Snapshot` returns a copy safe to serialize. `GetState` reads the state under a lock. The allowed transitions form a chain (`IDLE → PREPARING → … → COMPLETED`); any state may move to `ROLLING_BACK` or `FAILED`; `COMPLETED` and `FAILED` are terminal.

**`rollback.go`** implements `ExecuteRollback`. It records the failure, sets `ROLLING_BACK`, cleans up the target Pod if one was created, restores the source only when the failure happened at or after `CUTOFF` or `FINALIZING`, updates metrics, and ends in `SOURCE_RESTORED` then `FAILED`, or directly in `FAILED` when compensation itself failed. It reports an incomplete compensation rather than claiming success.

**`workload.go`** defines the `WorkloadController` interface (validate source and target, prepare target, stop source, verify target, restore source, clean up target) and three implementations. `PodWorkloadController` works on standalone Pods: it refuses controller-owned Pods and Pods using PVC or hostPath volumes, creates `<source>-target` pinned to the target node, and never overwrites an existing Pod. `StatefulSetWorkloadController` is a stub: `PrepareTarget` only returns a name and `VerifyTarget` returns nil without checking; it is unreachable because startup rejects the mode. `MockWorkloadController` simulates all operations for tests and mock mode.

**`monitor.go`** implements `RealTimeRateMonitor`. `RecordArrival` and `RecordProcessing` store timestamped counts. `MeasureRates` computes lambda and mu over the window, prefers the consumer's `/state` `messages_processed` delta when `CONSUMER_STATUS_URL` is set, falls back to the default mu only when measured mu is near zero, and reads queue depth from RabbitMQ.

**`api.go`** is the HTTP API: `POST /migrations` (validates JSON, returns 202, 400 for invalid input, 409 for conflicts), `GET /migrations`, `GET /migrations/{id}`, `GET /health`, and `GET /metrics`. Each handler is documented in the source. Server timeouts are 15 s for read and write.

**`metrics.go`** holds `MetricsCollector`: atomic counters for attempts, successes, failures, and message counts, plus gauges for the last migration's durations, downtime, checkpoint size, rates, and queue depth. `RenderPrometheus` emits them in text format. Labels are not used, so cardinality is fixed.

**`manager_test.go`** runs the mock workflow end to end, checks that a checkpoint failure triggers rollback to `FAILED`, checks source validation failure, and checks that a concurrent migration for the same source is rejected.

**`api_test.go`** checks POST status codes: 400 for a missing source, 202 for a valid start, 409 for a duplicate active source.

**`state_test.go`** checks the state machine's allowed and rejected transitions.


### 5.4 Checkpointing (`pkg/checkpoint/`)

**`provider.go`** defines the contract. `CheckpointResult` holds the namespace, pod, container, node, artifact path, size, duration, and SHA-256 checksum. `Target` names the restore destination. `CheckpointProvider` has `CreateCheckpoint` and `RestoreCheckpoint`. `RestorePreflight` is an optional interface a provider can implement to reject an unsupported restore before the workload is started.

**`mock.go`** implements `MockCheckpointProvider`. `CreateCheckpoint` writes a real tar file containing a text state record and 100 KB of zero bytes, and returns its checksum. This is **synthetic data, not a process checkpoint**. `RestoreCheckpoint` and `ValidateRestore` only check that the artifact exists and that a target node is named; they restore nothing.

**`kubelet.go`** implements `KubeletFCCProvider`, the real checkpoint client. `CreateCheckpoint` looks up the pod's node, then calls `POST /api/v1/nodes/<node>/proxy/checkpoint/<ns>/<pod>/<container>` through the API server (the node-proxy path). It parses `{"items":[path]}`, then tries to stat and checksum the archive. That read only succeeds if the archive is visible to the manager process. **`RestoreCheckpoint` always fails**: `ValidateRestore` returns an explicit error saying restore is not implemented. This is deliberate, so no migration can report a restored target through this path. `fileSHA256` computes the digest.

**`podman_restore.go`** implements `PodmanRestorer`, a node-side helper. `Restore` checks the archive exists and is non-empty, runs `podman container restore --import <archive> --name <name>` through an injected `CommandRunner`, and returns the container ID only if podman printed one. It is verified for podman-format archives. It does **not** read Kubelet or containerd archives.

**`provider_test.go`** tests mock creation and restore, mock failure injection, and that the Kubelet provider refuses restore.

**`podman_restore_test.go`** tests the restorer with a fake runner: correct arguments, rejection of invalid inputs without running anything, command failure propagation, and the empty-ID check.

### 5.5 Transfer (`pkg/transfer/`)

**`transfer.go`** defines `TransferProvider` and `TransferResult`. `FileTransferProvider.Transfer` copies the artifact to a destination directory while computing SHA-256, then checks the checksum and the destination size. It now refuses to copy a file onto itself (a bug that could truncate a checkpoint, found and fixed in this project). `MockTransferProvider` simulates a transfer, optionally with a delay or failure.

**`transfer_test.go`** covers a successful copy, a checksum mismatch, and the same-directory regression.

### 5.6 Cutoff and feasibility (`pkg/ms2m/`)

**`cutoff.go`** implements the adaptive cutoff. `CalculateAdaptiveCutoff` computes `T_cutoff = T_replay_max * (mu / lambda)`, bounded to `[min, max]`, with explicit handling for near-zero arrival (use max), non-positive processing (use min), NaN/Inf/negative inputs (use min), and congestion (`mu <= lambda`, reported as a warning). `CalculateThreshold` is a backward-compatible wrapper.

**`feasibility.go`** computes `rho = lambda/mu` and classifies it: `INFEASIBLE` at rho ≥ 1.0, `HIGH_RISK` at rho ≥ 0.80, otherwise `FEASIBLE`. `EvaluateFeasibility` applies the policy: `reject` blocks infeasible migrations, `warn` and `force` allow them.

**`cutoff_test.go`** and **`feasibility_test.go`** cover normal rates, idle traffic, overload, zero and invalid values, overflow, and backward compatibility.

### 5.7 Configuration and Kubernetes (`pkg/config/`, `pkg/k8s/`)

**`config/config.go`** defines `Config` and `Load`, which reads every variable from the environment with a default. `Validate` rejects unknown modes and providers, rejects `statefulset`, rejects `pod` mode with the mock checkpoint, rejects non-positive timeouts, and checks cutoff bounds. Helpers: `getEnv`, `getDurationEnv`, `getIntEnv`, `getBoolEnv`.

**`config/config_test.go`** checks the defaults are explicit mock settings and that unsafe mode combinations are rejected.

**`k8s/client.go`** builds a Kubernetes client: in-cluster config first, then `KUBECONFIG` or `~/.kube/config`. It exposes `GetPod`, `GetNode`, and `StopPod`. `RequestCheckpoint` deliberately returns an error, because checkpoints must go through the checkpoint provider. There are no tests for this package.


### 5.8 Containers and deployment

**`build/Dockerfile.manager`, `build/Dockerfile.producer`, `build/Dockerfile.consumer`, `build/Dockerfile.game2048`** are multi-stage builds. Each compiles one `cmd/` binary in a `golang:1.26-alpine` builder with `CGO_ENABLED=0`, then copies it into an `alpine:3.20` runtime that runs as a non-root user. `go mod download` is cached by copying `go.mod` and `go.sum` first.

**`docker-compose.yml`** runs four services on one network: `rabbitmq` (management image, healthcheck, persistent volume), `game2048` (port 8081), `migration-manager` (port 8080, mock modes, `CONSUMER_STATUS_URL` pointing at `game2048`), and `producer` (publishes `game/moves.jsonl`). `game2048` is the only workload used in Compose.

**`k8s/rbac.yaml`** defines the manager's ServiceAccount, a namespaced Role that can get, create, and delete Pods, and a ClusterRole that can get Nodes and create `nodes/proxy` requests for Kubelet checkpoints.

**`k8s/migration-manager.yaml`** is a Deployment and ClusterIP Service for the manager, running in mock mode with a read-only root filesystem and dropped capabilities.

**`k8s/rabbitmq.yaml`** is a single-replica RabbitMQ Deployment and Service. Credentials come from the `rabbitmq-credentials` Secret.

**`k8s/producer.yaml`** is a producer Deployment. It sets `MESSAGE_PAYLOAD` so it does not exit at startup.

**`k8s/consumer-statefulset.yaml`** is a two-replica consumer StatefulSet with a headless Service. It still uses the consumer image, not `game2048`.

**`k8s/kind-checkpoint.yaml`** defines the three-node kind cluster (one control plane, two workers) used for Kubelet checkpoint testing.

### 5.9 Scripts and sample data

**`scripts/criu_probe.sh`** checks checkpoint prerequisites in WSL: kernel config options, presence of CRIU and runtimes, and the user's groups. It is read-only.

**`scripts/podman_ckpt_smoke.sh`** runs a rootful podman checkpoint and restore of a counter container and checks that the counter resumes. It is the verified standalone checkpoint path.

**`scripts/kind_node_criu.sh`** builds CRIU 4.2 from upstream source inside a kind node and links it onto the PATH. It is the step that made the Kubelet checkpoint work. The current build fails at `images/stats.pb-c.c` on a fresh node, which is the open problem noted in section 6.

**`scripts/kind_node_criu_errors.sh`** and **`scripts/kind_node_criu_status.sh`** print the build errors and the install state of a node's CRIU.

The `node_*` scripts below are one-off diagnostics from the restore investigation. They are kept until live restore works, then should be removed. **`scripts/node_restore_probe.sh`** and **`scripts/node_restore_probe2.sh`** list the containerd and crictl subcommands and binary strings related to checkpoint and restore.

**`scripts/node_restore_help.sh`** prints the usage of `ctr containers checkpoint` and `ctr containers restore`.

**`scripts/node_live_restore_test.sh`** starts a counter task under `ctr` in a kind node, as the first step of a live restore test.

**`scripts/node_live_restore_check.sh`** checkpoints that task and attempts `ctr containers restore --live`, printing the exit code and output of each call.

**`scripts/benchmark.go`** runs a mock simulation over lambda values of 4 to 20 msg/s and writes CSV and JSON. It uses synthetic rates and is not a cluster measurement.

**`scripts/run_benchmark.ps1`** runs `benchmark.go` and reports success or failure.

**`scripts/generate_guide.py`** is an earlier generator for a file guide. It has not been run against the current code.

**`game/moves.jsonl`** is the 16-move script the producer cycles through.

**`.env`** holds the local mock-mode settings used by the `go run` instructions. It is Git-ignored.

**`.gitignore`** excludes binaries, test output, `.env`, the Go caches, and generated benchmark results.

## 6. Open problems

1. **The CRIU 4.2 build on a fresh node fails.** `make` reports `No rule to make target 'images/stats.pb-c.c'`. The earlier successful build had a git checkout that generated protobuf files. Until CRIU is built, the node cannot checkpoint.
2. **Restore of Kubelet archives is not implemented.** `KubeletFCCProvider.RestoreCheckpoint` refuses on purpose. The containerd restore interface (`ctr containers restore --live`) exists, but it has not been tested against a checkpoint image.
3. **Migrations do not move a live process.** The default and Compose modes are mock. Pod mode with the Kubelet provider cannot complete a restore.
4. **Duplicate detection is in memory**, so it does not survive a restart.
5. **Test binaries are blocked on this Windows machine** for some packages by Application Control. The code compiles and `go vet` passes; the tests must run where that policy allows them.
6. **The HTTP API has no authentication.**

| Benchmarks | Mock simulation only | Not a cluster measurement |
