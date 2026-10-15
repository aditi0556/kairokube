#!/usr/bin/env bash
set -euo pipefail

# Fedora host prerequisites for a real KairoKube checkpoint cluster.
# Run this on every Kubernetes node as root. It does not create a cluster.

if [[ "${EUID}" -ne 0 ]]; then
  echo "run as root: sudo $0" >&2
  exit 1
fi

# Fedora 44 publishes versioned Kubernetes and CRI-O packages. Keep the
# Kubernetes minor version aligned with CRI-O and cri-tools.
K8S_MINOR="${K8S_MINOR:-1.37}"

dnf install -y \
  "cri-o${K8S_MINOR}" \
  "cri-tools${K8S_MINOR}" \
  "kubernetes${K8S_MINOR}" \
  "kubernetes${K8S_MINOR}-kubeadm" \
  "kubernetes${K8S_MINOR}-client" \
  criu \
  containernetworking-plugins iproute-tc

modprobe overlay
modprobe br_netfilter

cat >/etc/sysctl.d/99-kairokube-kubernetes.conf <<'EOF'
net.ipv4.ip_forward = 1
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
EOF
sysctl --system

mkdir -p /etc/crio/crio.conf.d
cat >/etc/crio/crio.conf.d/99-kairokube-checkpoint.conf <<'EOF'
[crio.checkpoint_restore]
# Enable only on patched CRI-O releases appropriate for this cluster.
container_level_enabled = "checkpoint_restore"
EOF

systemctl daemon-reload
systemctl enable --now crio
systemctl enable kubelet

echo "CRI-O: $(crio --version | head -n 1)"
echo "CRIU: $(criu --version | head -n 1)"
echo "Runtime socket: /var/run/crio/crio.sock"
echo "Next: configure CNI, run kubeadm init/join, and verify ContainerCheckpoint."
