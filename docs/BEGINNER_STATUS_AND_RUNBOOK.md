# KairoKube: Beginner Status and Runbook

This document explains what KairoKube is, what has been changed, what is currently verified, and how to continue safely on Fedora.

## 1. What KairoKube does

KairoKube is a research prototype for moving a stateful workload between Kubernetes nodes.

The intended migration flow is:

1. The migration manager checks that the source and target are valid.
2. Kubernetes Kubelet asks the container runtime to checkpoint the source process.
3. The checkpoint is packaged and transferred through an OCI container registry.
4. The target runtime restores the checkpoint.
5. RabbitMQ messages that arrived during migration are replayed.
6. KairoKube verifies the target and completes the cutover.

The project also contains a 2048 workload, a message producer, RabbitMQ integration, a live dashboard, and a mock mode for development.

## 2. Current implementation status

| Area | Current status |
|---|---|
| Migration manager API | Implemented and unit tested |
| Dynamic dashboard | Implemented at `/dashboard` |
| RabbitMQ producer/consumer | Implemented |
| 2048 workload logic | Implemented and tested |
| Mock migration workflow | Works for local development |
| Local file transfer | Implemented with SHA-256 verification |
| Generic OCI/ORAS transfer | Implemented |
| CRI-O checkpoint-image transfer | Implemented in the code path using Buildah |
| Kubelet checkpoint request | Implemented; requires a suitable Kubernetes node |
| Docker-native checkpoint helper | Available for Linux Docker experiments only |
| StatefulSet migration | Not supported; controller-owned Pods are rejected |
| Real CRI-O restore | Code path is present, but has not been verified end to end on a real Fedora cluster |
| Full live migration of 2048 | Not yet verified |

Do not describe a migration as successful merely because the API reaches `COMPLETED` in mock mode. Mock mode creates a synthetic archive and does not move a real process.

## 2.1 Complete issue and problem register

This section records the known problems discovered during implementation and testing. An item marked **fixed** is kept here so that the original failure is not accidentally reintroduced.

### Fixed repository problems

- The dashboard was replaced with the dynamic KairoKube control-plane dashboard.
- Duplicate and one-off diagnostic files were removed from the repository.
- The Compose base image was corrected to an available Go Alpine image.
- The producer was fixed so it has a valid payload and does not exit immediately.
- The file-transfer provider was fixed so copying an artifact onto itself cannot truncate it.
- HTTP validation and conflict responses were corrected; invalid requests return client errors instead of always returning HTTP 500.
- The old Podman restore implementation was removed. Docker is retained only as an optional local/image-building path.
- Registry transfer support and CRI-O-oriented checkpoint-image code were added.
- Runtime validation now rejects Docker/Podman as the Kubernetes CRI checkpoint runtime and accepts CRI-O or containerd for an explicitly validated setup.

### Fedora and cluster setup problems

- `kubeadm`, `kubectl`, and `crictl` may not be installed by default on Fedora. Fedora exposes versioned packages such as `kubernetes1.37-kubeadm`, `kubernetes1.37-client`, and `cri-tools1.37`; Kubernetes, CRI-O, and cri-tools minor versions must be kept compatible.
- `crictl` access to `/var/run/crio/crio.sock` can fail with `permission denied` for a normal user. This is expected socket protection; use `sudo crictl --runtime-endpoint=unix:///var/run/crio/crio.sock info`.
- `/etc/kubernetes/admin.conf` does not exist before a successful `kubeadm init`. Copying it earlier always fails with `cannot stat`.
- `kubeadm init --config <file> --cri-socket <socket>` fails because the socket is already defined in `k8s/kubeadm-crio-config.yaml`. Do not pass `--cri-socket` a second time.
- `swapoff -a` was killed on the Fedora host. This commonly means the kernel ran out of memory while moving swap pages, or that zram/system resource policy interfered. Check RAM, active swap, and OOM logs before retrying.
- A CNI plugin is still required after kubeadm. Without a working CNI, nodes and Pods will not become Ready and CRIU networking will not work.
- The prerequisite script configures packages and CRI-O but does not create a cluster, install a CNI, create a registry, or execute the worker join command.
- A single Fedora PC may not have enough RAM for a control plane, multiple workers, RabbitMQ, registry access, and a live workload. Use additional Linux VMs or reduce the cluster size for development.

### Docker, kind, and CRIU problems

- Docker Desktop/kind uses a containerized node kernel and runtime. It is useful for dashboard and mock testing, but it is not evidence that a real Fedora CRI-O cluster can checkpoint and restore.
- The previous kind experiment failed in CRIU while creating a veth pair with `Operation not supported`. This is a Docker Desktop/LinuxKit networking limitation, not a normal application failure.
- Older CRIU packages can fail with vDSO errors such as `Unexpected rt vDSO area bounds`. CRIU must be compatible with the host kernel; upgrading CRIU alone cannot fix a kernel or namespace limitation.
- A source build of CRIU 4.2 previously failed on a fresh node while generating `images/stats.pb-c.c`. This points to missing protobuf build dependencies or an incomplete source tree. Prefer a tested Fedora package where possible, or build CRIU with all required protobuf generators.
- CRIU requires kernel support for checkpoint/restore, PID and network namespaces, veth creation, cgroups, and related networking features. The application cannot enable missing kernel capabilities.
- Docker-native checkpoint commands and Kubernetes Kubelet checkpoints are different formats and workflows. A Kubelet/CRI-O checkpoint must not be restored with Docker's native `docker start --checkpoint` command.

### Checkpoint and restore limitations

- The Kubelet provider can request a checkpoint, but the returned archive is created on the source node. The manager may not be able to read that node-local path from inside its own Pod. A production deployment needs a node-side agent, carefully scoped hostPath, or another explicit source-node transport.
- The generic Kubelet provider's restore method is intentionally not implemented. It refuses rather than claiming that a checkpoint was restored.
- The CRI-O restore provider is an orchestration path: CRI-O performs the restore when a target Pod is created from a correctly annotated checkpoint image. It is not a generic archive extractor.
- The current target preparation flow can create a normal target Pod before changing its image to the checkpoint image. That can race with scheduling, image pulling, and container startup. Production CRI-O restore should create the target with the checkpoint image from the beginning.
- Buildah must be available where the CRI-O checkpoint image is assembled. The manager image currently assumes the required transfer tooling is available in the execution environment; this needs a node/sidecar design for a hardened deployment.
- The checkpoint image annotation, CRI-O version, image media types, registry authentication, and target Pod specification must all match. A registry push succeeding does not prove that CRI-O can restore the image.
- The source workload must be a standalone Pod for the current controller. StatefulSet-owned Pods, PVCs, and hostPath-dependent workloads are rejected or unsafe for the current implementation.
- Network connections, open files, kernel resources, devices, and external volumes may not be checkpointable or portable between nodes. The 2048 workload is a test workload, not proof that arbitrary applications can migrate.

### Application and migration correctness risks

- Mock mode uses synthetic checkpoint data and does not move a process.
- The full live 2048 migration has not been completed and verified on a real Fedora multi-node CRI-O cluster.
- RabbitMQ duplicate detection is in memory and is lost on restart. It is not a durable exactly-once guarantee.
- RabbitMQ publishing does not use publisher confirms, so a broker failure window can still lose a published message.
- Migration state is held in process memory. Restarting the manager loses the in-flight migration records and metrics.
- Rollback is implemented and reports incomplete compensation, but rollback across a real runtime failure, registry failure, or node failure still needs fault-injection testing.
- A target Pod can be scheduled successfully while its restored process is not healthy. Readiness, state parity, and message replay must be verified before cutover.
- StatefulSet migration is not implemented. `MIGRATION_MODE=statefulset` is rejected rather than partially supported.
- The adaptive feasibility calculation can reject migrations when measured processing rate is zero or too low. In a quiet test, `mu=0` can be a measurement condition rather than proof that the application cannot migrate; use an explicit test workload and traffic.
- The benchmark is a mock simulation and must not be presented as a cluster performance measurement.

### Security and production-operation problems

- The HTTP API and dashboard have no built-in authentication or authorization. Put them behind an authenticated ingress or private network before production use.
- Registry passwords and tokens must be supplied through Kubernetes Secrets or an external secret manager. They must not be committed to `.env`, manifests, logs, or shell history.
- CRI-O checkpoint/restore is security-sensitive. Use patched, supported Fedora/CRI-O packages and review current security advisories before enabling restore.
- The current RBAC permissions include node proxy access for checkpointing. Review and minimize these permissions before production deployment.
- The manager needs a reliable, auditable way to access checkpoint files and registry credentials. Broad host mounts or privileged containers would increase risk.
- TLS, registry certificate trust, image retention, garbage collection, rate limits, audit logs, and resource quotas still need deployment-specific configuration.
- The default Compose credentials are development credentials only and must not be reused in production.

### Tooling and shutdown problems

- The agent session could not stop host services because it could not enter the user's `sudo` password and could not access the user's Docker Desktop socket. No KairoKube processes were visible in the agent session, but host state must be checked from the Fedora terminal.
- `docker compose down` stops the Compose containers but does not stop Fedora's kubelet or CRI-O services. `sudo systemctl stop kubelet crio` is required for those services.
- Stopping CRI-O stops CRI-O-managed containers but does not delete cluster state, images, registry data, or volumes.
- `docker compose down -v` is destructive for the Compose RabbitMQ data volume and should only be used intentionally.
- `kubeadm reset` is destructive to the node's Kubernetes configuration and should not be used as a first troubleshooting step.
- Running commands copied from formatted chat can add duplicate backslashes or duplicate flags. Enter each shell line with one trailing `\` only when the command continues on the next line.

## 3. Important runtime rule

Docker is useful for building images and running the local demo. It is not the recommended runtime for real Kubernetes checkpoint/restore.

For a real checkpoint experiment use:

- Fedora or another real Linux host/VM.
- Kubernetes with CRI-O.
- CRIU installed on every worker.
- A patched CRI-O release with checkpoint/restore support enabled.
- Kubernetes `ContainerCheckpoint` enabled.
- A registry reachable by both source and target nodes.

Docker Desktop, kind, and Docker-backed Kubernetes are suitable for dashboard and mock-mode development, but their network namespace limitations can prevent CRIU checkpointing.

## 4. Fedora files added for the real cluster

- `scripts/fedora_crio_prereq.sh`: installs version-aligned CRI-O, Kubernetes, CRI tools, CRIU, and networking packages; configures kernel networking and CRI-O checkpoint/restore settings.
- `k8s/kubeadm-crio-config.yaml`: kubeadm configuration with the CRI-O socket and `ContainerCheckpoint=true` feature gates.
- `scripts/push_images.sh`: builds and pushes KairoKube images and copies the public 2048 image when `skopeo` is available.
- `k8s/registry-kustomization.yaml`: image substitutions for a private or public OCI registry.

These scripts are not a substitute for checking the versions and security advisories applicable to the installed Fedora packages.

## 5. Fedora cluster setup

Run the prerequisite script on every node, including the control-plane node:

```bash
cd ~/Documents/KairoKube
sudo K8S_MINOR=1.37 bash scripts/fedora_crio_prereq.sh
```

Check the services and tools:

```bash
sudo systemctl status crio --no-pager
criu --version
sudo crictl --runtime-endpoint=unix:///var/run/crio/crio.sock info
```

The CRI-O socket is root-protected. A normal user may receive `permission denied`; use `sudo crictl` instead of making the socket world-readable.

### Initialize the control plane

The CRI-O socket is already in the kubeadm configuration. Do not pass a second `--cri-socket` argument together with `--config`.

First disable swap. If `swapoff` is killed, stop and diagnose memory pressure; do not continue to kubeadm until this is resolved.

```bash
sudo swapoff -a
sudo kubeadm init \
  --config=/home/Ds0uz4/Documents/KairoKube/k8s/kubeadm-crio-config.yaml
```

The error `can not mix '--config' with arguments [cri-socket]` means that `--cri-socket` was supplied twice. The correct command above supplies it only inside the YAML file.

If `swapoff -a` prints `Killed`, collect diagnostics:

```bash
free -h
swapon --show
sudo dmesg -T | grep -i -E 'oom|out of memory|killed process' | tail -30
```

That normally indicates that the kernel ran out of usable RAM while moving swap pages. It is a host-resource problem, not a KairoKube application error.

After a successful init, configure kubectl:

```bash
mkdir -p "$HOME/.kube"
sudo cp /etc/kubernetes/admin.conf "$HOME/.kube/config"
sudo chown "$(id -u):$(id -g)" "$HOME/.kube/config"
kubectl get nodes -o wide
```

The file `/etc/kubernetes/admin.conf` does not exist until `kubeadm init` completes successfully.

Install a CNI plugin before expecting nodes to become `Ready`. Use the CNI plugin's official installation instructions for the Kubernetes version being used. Then run the worker join command printed by kubeadm on every worker.

## 6. Registry and image preparation

Set a registry repository that every node can read. For Docker Hub, for example:

```bash
export REGISTRY=docker.io/<your-user>/kairokube
export CONTAINER_ENGINE=podman
bash scripts/push_images.sh
```

The script builds and pushes the manager, producer, and consumer images. It copies `jgreat/2048:latest` when `skopeo` is installed.

For a private registry, create an image-pull secret and reference it from the Kubernetes workload manifests. Never commit registry passwords or access tokens.

## 7. Configuration modes

### Local demo

Compose uses safe demo/mock providers:

```bash
docker compose up --build
```

Open `http://localhost:8080/dashboard`.

### Real Kubernetes checkpoint attempt

The relevant settings are:

```env
EXECUTION_MODE=full
MIGRATION_MODE=pod
CHECKPOINT_PROVIDER=kubelet
TRANSFER_PROVIDER=crio-registry
RESTORE_PROVIDER=crio
CHECKPOINT_REGISTRY_REPOSITORY=<registry>/<repository>
```

Use these only after the CRI-O, CRIU, kernel, CNI, registry, and Kubernetes feature-gate checks pass.

`TRANSFER_PROVIDER=registry` is the generic archive transfer path. `TRANSFER_PROVIDER=crio-registry` creates an annotated checkpoint image intended for CRI-O restore.

## 8. API and dashboard

The migration manager exposes:

- `GET /health`: process health.
- `GET /dashboard`: dynamic control-plane UI.
- `POST /migrations`: starts a migration.
- `GET /migrations`: lists migrations.
- `GET /migrations/{id}`: returns migration state.
- `GET /metrics`: Prometheus-compatible metrics.

Example request:

```bash
curl -X POST http://localhost:8080/migrations \
  -H 'Content-Type: application/json' \
  -d '{"source_pod":"game2048","namespace":"default","target_node":"worker-2"}'
```

A stateful StatefulSet Pod is intentionally rejected. Use a standalone Pod for the current migration controller.

## 9. What is not yet production complete

The real CRI-O path still needs an end-to-end validation on a real Fedora multi-node cluster. In particular:

1. A Kubelet checkpoint archive is created on the source node, so the manager must have a reliable node-side transport for that file.
2. A CRI-O checkpoint image must be created with the correct annotation and consumed by the target Pod creation flow.
3. The target should be created with the checkpoint image from the beginning; changing an already-created Pod image can race with normal scheduling and startup.
4. Registry authentication, TLS, image retention, rollback, and failure recovery need operational testing.
5. The dashboard and API currently do not provide authentication; place them behind an authenticated ingress before production use.

The repository code and tests cover the interfaces and validation, but they do not prove that every Fedora kernel/CRI-O combination can restore a live process.

## 10. Safe shutdown

### Stop the local Compose stack

```bash
docker compose down
```

Add `-v` only if you intentionally want to delete the Compose RabbitMQ data volume.

### Stop the Fedora Kubernetes services

This stops services but does not delete the cluster:

```bash
sudo systemctl stop kubelet
sudo systemctl stop crio
```

To check for remaining project processes:

```bash
ps -eo pid,user,comm,args | grep -E 'kairo|migration-manager|game2048|producer|consumer' | grep -v grep
```

Stopping CRI-O stops containers managed by CRI-O. It does not remove images, manifests, or registry data.

## 11. Verification commands

Run code tests from the repository:

```bash
GOPATH=/tmp/kairokube-gopath \
GOMODCACHE=/tmp/kairokube-gomodcache \
GOCACHE=/tmp/kairokube-go-cache \
go test ./...
```

Inspect the cluster and runtime:

```bash
kubectl get nodes -o wide
kubectl get pods -A
sudo crictl --runtime-endpoint=unix:///var/run/crio/crio.sock info
criu --version
```

If the cluster is not initialized, `kubectl` may be installed while `/etc/kubernetes/admin.conf` is absent. That is expected until kubeadm succeeds.

## 12. Files intentionally removed or replaced

Obsolete Podman restore code and duplicate/one-off diagnostic files were removed during the cleanup. Docker remains optional for image building and local demo use. The production-oriented direction is Fedora + CRI-O + CRIU, not Docker Desktop.
