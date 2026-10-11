#!/usr/bin/env bash
# Prints the first compile/link error lines from the CRIU build log in a kind node.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c 'grep -nE "error|Error|fatal" /tmp/criu-build.log | head -12'
