#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
task_root="$(mktemp -d)"
trap 'rm -rf -- "$task_root"' EXIT
export ZOE_BIN_DIR="$task_root/path/bin"
for release in first second; do
  mkdir -p "$task_root/$release/bin"
  printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$release" > "$task_root/$release/bin/zoe"
  chmod 0755 "$task_root/$release/bin/zoe"
done
bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/first"
[[ "$(PATH="$ZOE_BIN_DIR:$PATH" zoe)" == first ]]
bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/second"
[[ "$(PATH="$ZOE_BIN_DIR:$PATH" zoe)" == second ]]
[[ "$(readlink "$ZOE_BIN_DIR/zoe")" == "$task_root/second/bin/zoe" ]]
if bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/missing"; then
  echo "expected missing release refusal" >&2; exit 1
fi
[[ "$(PATH="$ZOE_BIN_DIR:$PATH" zoe)" == second ]]
rm "$ZOE_BIN_DIR/zoe"
printf 'operator-owned file\n' > "$ZOE_BIN_DIR/zoe"
if bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/first"; then
  echo "expected refusal to overwrite regular executable" >&2; exit 1
fi
[[ "$(cat "$ZOE_BIN_DIR/zoe")" == 'operator-owned file' ]]
echo 'CLI PATH installation tests passed'
