#!/usr/bin/env bash
# Shows the Makefile rule that should create images/stats.pb-c.c, and the build log's
# first make error, inside a kind node.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  echo "== rule for stats.pb-c.c (images/Makefile):"
  grep -n "stats" /tmp/criu-src/images/Makefile | head -8
  echo "== where stats is listed as proto:"
  grep -n "proto-obj-y\|PROTO\|proto-" /tmp/criu-src/images/Makefile | head -12
  echo "== first make error:"
  grep -n -m1 -B2 "No rule to make target" /tmp/criu-build.log
  echo "== git repo in source tree?"
  ls -d /tmp/criu-src/.git 2>/dev/null || echo no
'
