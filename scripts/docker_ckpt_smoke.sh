#!/usr/bin/env bash
set -euo pipefail

# Docker checkpoint/restore smoke test. Requires a Linux Docker Engine with
# CRIU support and the experimental checkpoint feature enabled.
NAME="kairokube-docker-ckpt-smoke"
CHECKPOINT_DIR="${TMPDIR:-/tmp}/kairokube-docker-checkpoints"
CHECKPOINT_NAME="checkpoint1"

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  rm -rf "$CHECKPOINT_DIR"
}
trap cleanup EXIT

mkdir -p "$CHECKPOINT_DIR"
docker pull -q alpine:3.20 >/dev/null
docker run -d --name "$NAME" alpine:3.20 sh -c 'i=0; while true; do echo "$i"; i=$((i+1)); sleep 1; done' >/dev/null
sleep 3

before=$(docker logs --tail 1 "$NAME")
echo "before checkpoint: $before"
docker checkpoint create --checkpoint-dir "$CHECKPOINT_DIR" "$NAME" "$CHECKPOINT_NAME" >/dev/null

docker start --checkpoint "$CHECKPOINT_NAME" --checkpoint-dir "$CHECKPOINT_DIR" "$NAME" >/dev/null
after=$(docker logs --tail 1 "$NAME")
echo "after restore:     $after"

if [[ "$after" -le "$before" ]]; then
  echo "RESULT: restore did not advance state" >&2
  exit 1
fi
echo "RESULT: Docker restored the checkpointed container and the counter resumed"
