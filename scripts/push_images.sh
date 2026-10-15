#!/usr/bin/env bash
set -euo pipefail

# Build and push workload images without requiring Docker. Podman is the
# default Fedora-native engine.

: "${REGISTRY:?set REGISTRY, for example registry.example.com/kairokube}"
ENGINE="${CONTAINER_ENGINE:-podman}"

command -v "$ENGINE" >/dev/null || {
  echo "container engine not found: $ENGINE" >&2
  exit 1
}

"$ENGINE" build -f build/Dockerfile.consumer -t "$REGISTRY/consumer:latest" .
"$ENGINE" build -f build/Dockerfile.producer -t "$REGISTRY/producer:latest" .
"$ENGINE" build -f build/Dockerfile.manager -t "$REGISTRY/manager:latest" .

"$ENGINE" push "$REGISTRY/consumer:latest"
"$ENGINE" push "$REGISTRY/producer:latest"
"$ENGINE" push "$REGISTRY/manager:latest"

# Copy the public game image directly when skopeo is available.
if command -v skopeo >/dev/null; then
  skopeo copy docker://jgreat/2048:latest "docker://$REGISTRY/2048:latest"
else
  echo "skopeo not installed; copy jgreat/2048:latest separately" >&2
fi
