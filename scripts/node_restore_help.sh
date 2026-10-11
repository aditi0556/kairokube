#!/usr/bin/env bash
# Prints the exact usage of ctr container checkpoint/restore in a kairokube node.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  echo "== ctr container restore --help"
  ctr container restore --help 2>&1
  echo "== ctr container checkpoint --help"
  ctr container checkpoint --help 2>&1
'
