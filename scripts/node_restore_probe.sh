#!/usr/bin/env bash
# Read-only probe of a kairokube kind node: which containerd/CRI tools can restore
# a checkpoint archive. Prints the help text of each candidate so the restore
# mechanism is taken from the installed binaries, not from memory or web docs.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  echo "== containerd: $(containerd --version)"
  echo "== crictl: $(crictl --version 2>&1)"
  echo "== ctr run flags mentioning checkpoint/restore:"
  ctr run --help 2>&1 | grep -iE "checkpoint|restore" || echo "  none"
  echo "== ctr top-level commands:"
  ctr --help 2>&1 | sed -n "/COMMANDS/,/GLOBAL/p" | head -40
  echo "== crictl commands:"
  crictl --help 2>&1 | sed -n "/Commands:/,/Options:/p" | head -60
  echo "== crictl checkpoint/restore related:"
  crictl --help 2>&1 | grep -iE "checkpoint|restore" || echo "  none"
'
