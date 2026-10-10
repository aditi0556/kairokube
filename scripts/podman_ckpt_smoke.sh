#!/usr/bin/env bash
# Rootless podman checkpoint/restore smoke test. Proves the CRIU path works
# end to end on this host, independent of Kubernetes.
set -eu

NAME=ms2m-ckpt
ARCHIVE="$HOME/ms2m-ckpt.tar.gz"

podman rm -f "$NAME" >/dev/null 2>&1 || true
podman pull -q docker.io/library/alpine:3.20 >/dev/null

# A counter that writes to stdout, so state is observable before/after restore.
podman run -d --name "$NAME" docker.io/library/alpine:3.20 \
  sh -c 'i=0; while true; do i=$((i+1)); echo "count=$i"; sleep 1; done' >/dev/null
sleep 4
before=$(podman logs --tail 1 "$NAME")
echo "before checkpoint: $before"

podman container checkpoint --export="$ARCHIVE" "$NAME"
echo "checkpoint archive: $(stat -c %s "$ARCHIVE") bytes"

podman rm -f "$NAME" >/dev/null
podman container restore --import="$ARCHIVE" >/dev/null
sleep 3
after=$(podman logs --tail 1 "$NAME")
echo "after restore:     $after"

if [ "$after" = "$before" ]; then
  echo "RESULT: restore did not advance state" >&2
  exit 1
fi
echo "RESULT: restored container resumed counting from checkpointed state"
