#!/usr/bin/env bash
# Reports the state of the CRIU build in a kind node: running make, errors, and install result.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  echo "make running: $(pgrep -c make)"
  echo "--- make errors:"
  grep -nE "No rule|\*\*\*" /tmp/criu-build.log | head -5
  echo "--- log tail:"
  tail -4 /tmp/criu-build.log
  echo "--- installed:"
  criu --version 2>&1 | head -1
'
