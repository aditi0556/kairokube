#!/usr/bin/env bash
# Read-only probe for checkpoint/restore prerequisites. Requires no root.
# Usage (WSL): wsl -d Ubuntu -- bash scripts/criu_probe.sh
set -u

echo "kernel: $(uname -r)"
echo "os: $(. /etc/os-release && echo "$PRETTY_NAME")"

if [ -r /proc/config.gz ]; then
  for opt in CHECKPOINT_RESTORE USER_NS PID_NS NET_NS CGROUPS MEMCG UNIX_DIAG INET_DIAG NETLINK_DIAG; do
    line=$(zcat /proc/config.gz | grep -E "^CONFIG_${opt}=" || true)
    echo "config ${opt}: ${line:-not set}"
  done
else
  echo "config: /proc/config.gz not readable"
fi

for tool in criu runc crun podman crictl containerd crio; do
  if command -v "$tool" >/dev/null 2>&1; then
    echo "tool ${tool}: $(command -v "$tool")"
  else
    echo "tool ${tool}: missing"
  fi
done

echo "user: $(id -un) groups: $(id -Gn)"
