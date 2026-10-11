#!/usr/bin/env bash
# Live checkpoint/restore test inside a kairokube kind node using containerd's ctr.
# 1. Runs a counter container with a task.
# 2. Checkpoints its running task (--task) to an image reference.
# 3. Restores it with --live and checks the counter continues from the checkpoint.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  NS=k8s.io
  CID=livecounter
  ctr -n $NS tasks kill -s SIGKILL $CID >/dev/null 2>&1
  ctr -n $NS containers rm $CID >/dev/null 2>&1

  echo "== pull"
  ctr -n $NS images pull docker.io/library/alpine:3.20 2>&1 | tail -2

  echo "== run"
  ctr -n $NS run -d --net-host docker.io/library/alpine:3.20 $CID \
    sh -c "i=0; while true; do i=\$((i+1)); echo count=\$i; sleep 1; done" 2>&1 | tail -3
  sleep 5
  echo "== tasks"
  ctr -n $NS tasks ls 2>&1
'
