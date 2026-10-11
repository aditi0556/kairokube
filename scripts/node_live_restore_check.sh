#!/usr/bin/env bash
# Checkpoints the running livecounter task and restores it with --live into a new
# container. Prints the exit code and output of each ctr call so failures are visible.
set -u
node=${1:-kairokube-worker}
podman exec "$node" sh -c '
  NS=k8s.io
  CID=livecounter
  REF=localhost/kairokube/livecounter-ckpt:v1
  RCID=livecounter-restored

  echo "== last log lines of the live task"
  ctr -n $NS tasks ls | grep livecounter

  echo "== checkpoint"
  ctr -n $NS containers checkpoint --task $CID $REF > /tmp/ckpt.out 2>&1
  echo "checkpoint exit=$?"; tail -3 /tmp/ckpt.out

  echo "== restore --live"
  ctr -n $NS containers delete $RCID >/dev/null 2>&1
  ctr -n $NS containers restore --live $RCID $REF > /tmp/restore.out 2>&1
  echo "restore exit=$?"; tail -3 /tmp/restore.out
'
