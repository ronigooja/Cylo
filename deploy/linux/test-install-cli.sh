#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
task_root="$(mktemp -d)"
trap 'rm -rf -- "$task_root"' EXIT
export CYLO_BIN_DIR="$task_root/path/bin"
for release in first second; do
  mkdir -p "$task_root/$release/bin"
  printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$release" > "$task_root/$release/bin/cylo"
  chmod 0755 "$task_root/$release/bin/cylo"
done
bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/first"
[[ "$(PATH="$CYLO_BIN_DIR:$PATH" cylo)" == first ]]
bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/second"
[[ "$(PATH="$CYLO_BIN_DIR:$PATH" cylo)" == second ]]
[[ "$(readlink "$CYLO_BIN_DIR/cylo")" == "$task_root/second/bin/cylo" ]]
if bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/missing"; then
  echo "expected missing release refusal" >&2; exit 1
fi
[[ "$(PATH="$CYLO_BIN_DIR:$PATH" cylo)" == second ]]
rm "$CYLO_BIN_DIR/cylo"
printf 'operator-owned file\n' > "$CYLO_BIN_DIR/cylo"
if bash "$repo_root/deploy/linux/install-cli.sh" "$task_root/first"; then
  echo "expected refusal to overwrite regular executable" >&2; exit 1
fi
[[ "$(cat "$CYLO_BIN_DIR/cylo")" == 'operator-owned file' ]]
echo 'CLI PATH installation tests passed'
