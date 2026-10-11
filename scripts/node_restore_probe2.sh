#!/usr/bin/env bash
# Read-only probe for a checkpoint-image restore path in a kairokube kind node.
# containerd can create a container from a checkpoint image; this checks whether
# the installed ctr/crictl/containerd expose that path.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  echo "== ctr container create flags mentioning checkpoint:"
  ctr container create --help 2>&1 | grep -iE "checkpoint" || echo "  none"
  echo "== ctr image subcommands:"
  ctr image --help 2>&1 | sed -n "/COMMANDS/,/GLOBAL/p" | head -25
  echo "== ctr container subcommands:"
  ctr container --help 2>&1 | sed -n "/COMMANDS/,/GLOBAL/p" | head -25
  echo "== crictl create help (checkpoint related):"
  crictl create --help 2>&1 | grep -iE "checkpoint|restore" || echo "  none"
  echo "== containerd binary: checkpoint media type strings:"
  grep -ao "vnd.containerd.container.criu[a-zA-Z.-]*" /usr/local/bin/containerd | sort -u | head -10 || true
  echo "== containerd binary: restore-related CRI strings:"
  grep -ao "CheckpointContainer[A-Za-z]*\|RestoreContainer[A-Za-z]*" /usr/local/bin/containerd | sort -u | head -10 || true
'
