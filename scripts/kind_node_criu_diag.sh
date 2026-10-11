#!/usr/bin/env bash
# Diagnoses why CRIU's protobuf codegen does not produce images/*.pb-c.c in a kind node.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  echo "== protoc-c: $(command -v protoc-c || echo MISSING)"
  echo "== protobuf-c-compiler: $(dpkg -s protobuf-c-compiler 2>/dev/null | grep ^Status || echo not-installed)"
  echo "== src present: $(ls /tmp/criu-src/images/stats.proto 2>/dev/null || echo no-stats-proto)"
  echo "== pb-c rule in Makefiles:"
  grep -rn "pb-c" /tmp/criu-src/Makefile /tmp/criu-src/images/Makefile* /tmp/criu-src/scripts/*.mk 2>/dev/null | head -8
  echo "== build log head:"
  head -5 /tmp/criu-build.log 2>/dev/null
'
