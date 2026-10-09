#!/usr/bin/env python3
"""Generate the repository file/function guide in Markdown."""

from __future__ import annotations

import re
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MD_PATH = ROOT / "docs" / "KairoKube_File_and_Function_Guide.md"
SKIP_PARTS = {".git", ".gocache", ".gomodcache", "vendor", "build-output", "__pycache__", "tmp"}


def files_in_repo() -> list[Path]:
    files = sorted(
        p for p in ROOT.rglob("*")
        if p.is_file() and not any(part in SKIP_PARTS for part in p.parts)
    )
    if MD_PATH not in files:
        files.append(MD_PATH)
    return sorted(files)


def go_functions(path: Path) -> list[dict[str, str]]:
    text = path.read_text(encoding="utf-8")
    starts = list(re.finditer(r"(?m)^func\s+(?:\([^\n]*?\)\s*)?([A-Za-z_]\w*)\s*\(", text))
    names = {m.group(1) for m in starts}
    entries = []
    for i, match in enumerate(starts):
        end = starts[i + 1].start() if i + 1 < len(starts) else len(text)
        body = text[match.start():end]
        line_start = text.rfind("\n", 0, match.start()) + 1
        prefix = text[:line_start]
        comment_lines = []
        for line in reversed(prefix.splitlines()):
            if line.strip().startswith("//"):
                comment_lines.append(line.strip()[2:].strip())
            elif line.strip() == "":
                if comment_lines:
                    break
            else:
                break
        comment = " ".join(reversed(comment_lines)).replace("≈", "approximately")
        signature = re.search(r"(?s)^func\s+.*?\{", body)
        sig = signature.group(0)[:-1].strip() if signature else match.group(0)
        calls = sorted(n for n in names if n != match.group(1) and re.search(rf"\b{re.escape(n)}\s*\(", body))
        hints = []
        if re.search(r"\.Lock\(|\.RLock\(|sync\.", body):
            hints.append("uses synchronization")
        if re.search(r"\bgo\s+", body):
            hints.append("starts a goroutine")
        if "ctx" in sig or "ctx." in body:
            hints.append("uses or propagates context cancellation")
        entries.append({
            "name": match.group(1),
            "signature": sig,
            "comment": comment or "Internal or test helper; behavior is summarized from its implementation and call sites.",
            "calls": ", ".join(calls) if calls else "No direct same-file function calls detected.",
            "hints": ", ".join(hints) if hints else "No concurrency-specific behavior detected by source scan.",
        })
    for entry in entries:
        entry["callers"] = ", ".join(fn["name"] for fn in entries if entry["name"] in fn["calls"].split(", ")) or "No direct same-file callers detected."
    return entries


def inventory_description(path: Path) -> tuple[str, str, str]:
    rel = path.relative_to(ROOT).as_posix()
    if rel == "go.mod": return "Module identity, Go version, and direct/indirect dependencies.", "Build configuration", "Go module system"
    if rel == "go.sum": return "Checksums for pinned Go module versions.", "Dependency lock", "Go module system"
    if rel == "README.md": return "Project overview, capability status, local setup, API, and limitations.", "Documentation", "All project components"
    if rel == ".gitignore": return "Excludes binaries, caches, benchmark outputs, and local build artifacts.", "Repository configuration", "Git"
    if rel.startswith("cmd/"): return "Executable entry point and process lifecycle for the named service.", "Go executable", "Packages under pkg/"
    if rel.startswith("pkg/config/"): return "Environment loading and supported configuration validation.", "Go package", "Migration manager and tests"
    if rel.startswith("pkg/checkpoint/"): return "Checkpoint interface, local mock archive, Kubelet FCC request, and provider tests.", "Go package", "Migration manager"
    if rel.startswith("pkg/transfer/"): return "Local file-copy and mock transfer providers plus checksums.", "Go package", "Migration manager"
    if rel.startswith("pkg/rabbitmq/"): return "Versioned message schema, AMQP client, durable queue operations, and bounded deduplication.", "Go package", "Producer, consumer, migration monitor"
    if rel.startswith("pkg/migration/"): return "Migration state, orchestration, workload controllers, rollback, API, monitor, and metrics.", "Go package", "Migration manager executable"
    if rel.startswith("pkg/ms2m/"): return "Cutoff calculation and feasibility policy.", "Go package", "Migration manager"
    if rel.startswith("pkg/k8s/"): return "Client-go configuration and basic Pod/Node helpers.", "Go package", "Migration manager and providers"
    if rel.startswith("k8s/"): return "Kubernetes resource definitions and runtime configuration.", "Deployment manifest", "Kubernetes cluster"
    if rel.startswith("build/"): return "Multi-stage container build definition.", "Container build", "Docker or compatible builder"
    if rel == "scripts/benchmark.go": return "Synthetic-rate mock migration scenario runner; not a cluster benchmark.", "Go utility", "Mock providers and migration manager"
    if rel == "scripts/run_benchmark.ps1": return "PowerShell wrapper for the mock benchmark runner.", "PowerShell utility", "Go toolchain"
    if rel == "scripts/generate_guide.py": return "Regenerates this Markdown guide from the final source tree.", "Documentation utility", "Python standard library"
    if rel.endswith("_test.go"): return "Automated unit tests for the adjacent package behavior.", "Go tests", "Go test"
    if rel.startswith("docs/"): return "Audit, limitations, or generated technical guide documentation.", "Documentation", "Project contributors"
    return "Project source or configuration; consult its package and in-file comments.", "Source/configuration", "Project components"


def make_markdown() -> str:
    files = files_in_repo()
    tree = "\n".join(f"- `{p.relative_to(ROOT).as_posix()}`" for p in files)
    out = [
        "# KairoKube File and Function Guide",
        "",
        "Generated: 2026-10-09  |  Project: KairoKube  |  Status: Research prototype",
        "",
        "## Purpose and status legend",
        "",
        "KairoKube explores message-based state reconstruction around Kubernetes container checkpointing. This guide describes the repository as it exists at generation time. `Implemented` means source code exists; `Mock` means simulated behavior; `Unsupported` means the system reports an explicit limitation; `Cluster-dependent` means this environment did not verify the behavior against a live cluster.",
        "",
        "**Current status:** mock orchestration and API are implemented. The Kubelet provider can request an FCC checkpoint when configured, but artifact access, remote transfer, and runtime restore are not a complete migration path. The provider now fails before target startup when runtime restore is unsupported. StatefulSet mode is rejected. Migration history is process-local.",
        "",
        "## Contents",
        "",
        "Sections below follow the order shown in this guide.",
        "",
        "## Architecture and lifecycle",
        "",
        "```text",
        "Producer -> RabbitMQ durable queue -> Consumer (manual ACK)",
        "                                |",
        "                                +-> Migration Manager API / orchestration",
        "                                     -> checkpoint provider -> transfer provider",
        "                                     -> restore preflight -> workload controller",
        "                                     -> replay/cutoff/finalization",
        "```",
        "",
        "Mock lifecycle: `IDLE -> PREPARING -> CHECKPOINTING -> CHECKPOINT_CREATED -> TRANSFERRING -> RESTORING -> REPLAYING -> CUTOFF -> FINALIZING -> COMPLETED`. Failure compensation uses `ROLLING_BACK -> SOURCE_RESTORED -> FAILED`. The manager rejects a second active migration for the same namespace/source Pod. The labels express orchestration states; they do not guarantee persistent recovery or live process restoration.",
        "",
        "The cutoff model is `T_cutoff <= T_replay_max * (mu / lambda)`, with configured lower and upper bounds. Rates are messages/second and times are seconds. In pod mode, absent processing samples remain zero. The 20 messages/second fallback is only present in explicitly selected mock mode. The model and measured window are not a guarantee that a broker queue will drain.",
        "",
        "## Repository tree",
        "",
        tree,
        "",
        "## File-by-file inventory",
        "",
        "| Path | Purpose | Category | Depends on / interacts with |",
        "|---|---|---|---|",
    ]
    for path in files:
        rel = path.relative_to(ROOT).as_posix()
        purpose, category, deps = inventory_description(path)
        out.append(f"| `{rel}` | {purpose} | {category} | {deps} |")

    out += ["", "## Function-by-function Go reference", "",
            "Each declaration is extracted from the final Go source. Signatures show parameters and results. The adjacent source comment is retained when present; same-file callers/callees are detected by static text scan, not a guaranteed complete inter-package call graph. Synchronization/context tags are implementation hints, not formal behavior guarantees.", ""]
    go_files = [p for p in files if p.suffix == ".go"]
    for path in go_files:
        rel = path.relative_to(ROOT).as_posix()
        funcs = go_functions(path)
        if not funcs:
            continue
        out += [f"### `{rel}`", ""]
        for fn in funcs:
            signature = "\n".join("\n".join(textwrap.wrap(part, width=100, break_long_words=False, break_on_hyphens=False, subsequent_indent="\t")) for part in fn["signature"].splitlines())
            out += [f"#### `{fn['name']}`", "", "```go", signature, "```", "",
                    f"{fn['comment']}", "",
                    f"**Same-file callers:** {fn['callers']}  ",
                    f"**Same-file callees:** {fn['calls']}  ",
                    f"**Concurrency/lifecycle:** {fn['hints']}", ""]

    out += [
        "## Message schema, acknowledgements, replay, and cutoff",
        "",
        "Version 1 messages contain `version`, unique `id`, producer sequence, UTC timestamp, and string `payload`. Decoder accepts an unversioned legacy object as version 1 and rejects unknown explicit versions. Sequences are only monotonic for one ordered producer; UUID identifies a message. Consumer ACK occurs after state update. A crash after state update but before ACK can redeliver the message. Deduplication is a bounded in-memory ID cache; process restart and eviction can allow old duplicates through. Durable idempotency is not implemented.",
        "",
        "The current manager waits for the calculated cutoff duration and does not connect replay to a recorded broker boundary or last processed sequence. The consumer and manager are not fenced against concurrent source/target application. Therefore at-least-once delivery does not establish exactly-once state effects or zero data loss.",
        "",
        "## Configuration reference",
        "",
        "| Variable | Default in `pkg/config.Load` | Required | Meaning / limitation |",
        "|---|---|---|---|",
        "| `RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | Yes for manager/producer/consumer | AMQP broker URL; deploy manifests reference a Secret instead of embedding credentials. |",
        "| `QUEUE_NAME` | `microservices-queue` | Yes | Durable queue name. |",
        "| `MIGRATION_MODE` | `mock` | Yes | `mock` for simulation; `pod` requires Kubernetes; `statefulset` is rejected. |",
        "| `MIGRATION_FEASIBILITY_POLICY` | `warn` | No | `warn`, `reject`, or `force`. |",
        "| `CHECKPOINT_PROVIDER` | `mock` | Yes | `mock` or Kubelet checkpoint creation. Real restore is unavailable. |",
        "| `TRANSFER_PROVIDER` | `mock` | Yes | `mock` or local/shared-path file copy; neither is remote transfer by itself. |",
        "| `MAX_REPLAY_TIME` | `5s` | No | Cutoff-model maximum replay duration. |",
        "| `MIN_CUTOFF_TIME` / `MAX_CUTOFF_TIME` | `1s` / `30s` | No | Bounds for computed cutoff. |",
        "| `METRICS_WINDOW` | `5s` | No | Rate measurement window. |",
        "| `CHECKPOINT_TIMEOUT` / `TRANSFER_TIMEOUT` / `RESTORE_TIMEOUT` | `30s` / `60s` / `60s` | No | Phase timeout configuration. |",
        "| `REPLAY_TIMEOUT` / `MIGRATION_TIMEOUT` | `30s` / `5m` | No | Replay and overall timeout configuration. |",
        "| `KUBELET_PORT` / `KUBELET_SCHEME` | `10250` / `https` | No | Kubelet direct-connection settings; proxy mode uses the API server. |",
        "| `CHECKPOINT_DIR` | OS temp directory + `kairokube-checkpoints` | No | Mock output/local transfer directory, not proof of remote node access. |",
        "| `SERVER_PORT` | `8080` | No | Manager HTTP listen port. |",
        "| `DEBUG_LOGGING` | `false` | No | Parsed config field; current logger does not apply a structured logging backend. |",
        "",
        "Example local simulation: `MIGRATION_MODE=mock CHECKPOINT_PROVIDER=mock TRANSFER_PROVIDER=mock RABBITMQ_URL=amqp://user:password@localhost:5672/`. Use Secret-backed URL values in a cluster.",
        "",
        "## HTTP API",
        "",
        "| Method and path | Success | Main errors |",
        "|---|---|---|",
        "| `POST /migrations` | `202` JSON `{\"migration_id\":\"...\",\"status\":\"PREPARING\"}` | `400` invalid/missing input; currently request decoder does not reject unknown fields. |",
        "| `GET /migrations` | `200` array of in-memory snapshots | No durable history; no pagination. |",
        "| `GET /migrations/{id}` | `200` snapshot | `404` unknown ID; `405` wrong method. |",
        "| `GET /health` | `200` health JSON | Process-only check; does not establish broker readiness. |",
        "| `GET /metrics` | `200` Prometheus text | Current counters/gauges; no per-migration labels. |",
        "",
        "Example request: `curl -X POST http://localhost:8080/migrations -H 'Content-Type: application/json' -d '{\"source_pod\":\"demo\",\"namespace\":\"default\",\"target_node\":\"worker-2\"}'`. No browser dashboard is included.",
        "",
        "## Container build and manifest details",
        "",
        "The three Dockerfiles use a pinned Go 1.26.8 Alpine builder and a minimal Alpine 3.22 runtime. Producer and consumer set a non-root user; the manager image also runs non-root. The manager manifest adds a read-only root filesystem, drops Linux capabilities, disables privilege escalation, mounts `/tmp` as `emptyDir`, and sets CPU/memory requests and limits. These image builds were not executed because the local Docker engine was inaccessible.",
        "",
        "| Manifest | Resources and significant settings |",
        "|---|---|",
        "| `k8s/migration-manager.yaml` | One Deployment and Service, API port 8080, mock provider/mode, health probes, secret-backed RabbitMQ URL, resource/security settings. |",
        "| `k8s/producer.yaml` | One producer Deployment; broker URL comes from `rabbitmq-credentials`. |",
        "| `k8s/consumer-statefulset.yaml` | Two-replica consumer StatefulSet with broker URL Secret reference. The manager does not support migrating StatefulSets. |",
        "| `k8s/rabbitmq.yaml` | Single RabbitMQ Deployment and Service exposing AMQP and management ports; credentials come from Secret; no persistent volume. |",
        "| `k8s/rbac.yaml` | ServiceAccount, namespace Role for Pod get/create/delete, ClusterRole for Node get and `nodes/proxy` create, and bindings. |",
        "",
        "## Deployment and external prerequisites",
        "",
        "Kubernetes manifests use the manager's explicit mock mode for a safe control-flow demo. They expect a pre-created `rabbitmq-credentials` Secret with `url`, `username`, and `password` keys. Example: `kubectl create secret generic rabbitmq-credentials --from-literal=url=\"$RABBITMQ_URL\" --from-literal=username=\"$RABBITMQ_USER\" --from-literal=password=\"$RABBITMQ_PASSWORD\"`. Never commit the resulting values.",
        "",
        "The Kubelet FCC endpoint is `POST /checkpoint/{namespace}/{pod}/{container}`. Kubernetes documents the API as Beta since v1.30 and enabled by default at the time of generation; actual access still depends on node/runtime support and Kubelet authorization. The API returns a node-local tar archive. KairoKube does not implement compatible CRI restore or target-side artifact delivery. Do not switch the sample manifest to pod/Kubelet mode expecting migration to complete. Review [Kubernetes Kubelet Checkpoint API](https://kubernetes.io/docs/reference/node/kubelet-checkpoint-api/) and [feature-gate configuration](https://kubernetes.io/docs/tasks/administer-cluster/configure-feature-gates/).",
        "",
        "RBAC grants Pod operations and Kubelet `nodes/proxy` capability. `nodes/proxy` is powerful; restrict the manager and validate the exact required verbs in the target cluster. StatefulSet migration is unsupported. Manifests reference a pre-created `rabbitmq-credentials` Secret with URL, username, and password keys. The sample RabbitMQ workload is one replica without persistent storage; it is only a development fixture.",
        "",
        "## Local development, benchmark, and operations",
        "",
        "Build: `go build ./...`. Tests: `go test ./...`. Static checks: `go vet ./...`; race checks: `go test -race ./...`. Start RabbitMQ separately, then run manager/producer/consumer with their environment settings. The benchmark runner injects synthetic arrivals and processing counts, uses mock checkpoint/workload/transfer providers, and must be labeled `mock_simulation_synthetic_rates`. It leaves queue depth, replay count, and duplicate count absent because they are not observed. Its phase timings are process measurements in a simulated flow, not cluster benchmark evidence.",
        "",
        "No destructive failure injection is wired to production settings. Shutdown drains the HTTP server; in-flight migration goroutines are not persisted and do not have manager-wide coordinated cancellation. Service message history and duplicate state are process-local.",
        "",
        "## Validation actually performed",
        "",
        "Validation executed for this generation: `gofmt -w cmd pkg scripts` completed; `go test ./...` passed; `go test -race ./...` passed; `go vet ./...` passed; `go build ./...` passed; a temporary Go YAML parser decoded every `k8s/*.yaml` document and confirmed `apiVersion`/`kind`; and `git diff --check` passed. `kubectl create --dry-run=client ...` could not validate manifests because the configured Kubernetes API server at `127.0.0.1:62033` is unavailable; client-only dry-run still attempted API discovery. Docker CLI is installed, but the Docker Desktop Linux engine pipe denied access, so image builds were not run. No live RabbitMQ or Kubernetes integration run was available. The Go commands first needed workspace/module-cache setup; the final command results above are successful and were obtained with module-cache write access. A mock test is not a live Kubelet/runtime integration test.",
        "",
        "## Known limitations and glossary",
        "",
        "- **ACK:** RabbitMQ consumer acknowledgement after processing; delivery remains at-least-once.",
        "- **CRIU:** Checkpoint/restore utility used by some container runtimes; KairoKube does not invoke a restore integration.",
        "- **Cutoff:** Chosen end to accumulation/replay interval; current implementation uses a duration, not a durable message boundary.",
        "- **Downtime:** Recorded duration from cutoff to completion; mock values do not represent production service availability.",
        "- **FCC:** Kubernetes Kubelet Forensic Container Checkpointing API for creating a container checkpoint archive.",
        "- **lambda:** Observed or simulated incoming messages per second.",
        "- **mu:** Observed or mock-baseline processed messages per second.",
        "- **MS2M:** Message-based State Reconstruction for Microservice Migration.",
        "- **Replay:** Reprocessing messages to reconstruct application state; no durable replay boundary is implemented.",
        "- **Checkpoint:** Archive returned by the Kubelet or synthetic tar emitted by the mock provider; only the former is a live checkpoint request.",
        "",
    ]
    return "\n".join(out)


def main():
    MD_PATH.parent.mkdir(parents=True, exist_ok=True)
    markdown = make_markdown()
    MD_PATH.write_text(markdown, encoding="utf-8")
    print(f"Wrote {MD_PATH} ({MD_PATH.stat().st_size} bytes)")


if __name__ == "__main__":
    main()