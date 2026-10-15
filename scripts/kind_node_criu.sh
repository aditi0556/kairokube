#!/usr/bin/env bash
# Installs CRIU 4.2, built from upstream source, into a kairokube kind node.
#
# Why: the Debian bookworm package is CRIU 3.17.1, which fails with
# "vdso: Unexpected rt vDSO area bounds" on the WSL2 kernel. CRIU 4.2 built here
# passes `criu check` and completes kubelet checkpoints.
#
# Usage (WSL, root): bash scripts/kind_node_criu.sh [node]
set -eu
node=${1:-kairokube-worker}

docker exec "$node" sh -c '
  set -e
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  # protoc (protobuf-compiler) generates images/*.pb-c.c during the build. The
  # protobuf-compiler package itself has no Java dependency; only the doc/Java
  # tooling we exclude pulled in a JRE that failed to configure in this image.
  mkdir -p /usr/share/man/man1
  apt-get install -y -qq --no-install-recommends build-essential git pkg-config \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf \
    libnl-3-dev libnl-genl-3-dev libcap-dev libnet1-dev libbsd-dev \
    libnftables-dev libgnutls28-dev libdrm-dev libselinux1-dev uuid-dev iptables \
    > /tmp/criu-deps.log 2>&1
  rm -rf /tmp/criu-src && mkdir -p /tmp/criu-src && cd /tmp/criu-src
  curl -sSfL https://github.com/checkpoint-restore/criu/archive/refs/tags/v4.2.tar.gz | tar xz --strip-components=1
  # The release tarball has no .git, and CRIU's Makefile runs git to derive the
  # version. Initialise a throwaway repo tagged v4.2 so the build is reproducible.
  git init -q . && git add -A >/dev/null && git -c user.email=b@local -c user.name=build commit -qm "criu 4.2" \
    && git tag v4.2
  make -j"$(nproc)" > /tmp/criu-build.log 2>&1
  make install PREFIX=/usr/local > /tmp/criu-install.log 2>&1
  # containerd and the kubelet resolve "criu" by name on PATH.
  ln -sfn /usr/local/sbin/criu /usr/local/bin/criu
  criu --version | head -1
  criu check 2>&1 | tail -1
'
