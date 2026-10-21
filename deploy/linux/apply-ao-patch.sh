#!/usr/bin/env bash
set -euo pipefail

expected="7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ao_source="${1:-}"
if [[ -z "$ao_source" || ! -e "$ao_source/.git" ]]; then
  echo "usage: $0 /path/to/agent-orchestrator-checkout" >&2
  exit 2
fi
actual="$(git -C "$ao_source" rev-parse HEAD)"
if [[ "$actual" != "$expected" ]]; then
  echo "AO source revision mismatch: expected $expected, found $actual" >&2
  exit 1
fi
patch="$repo_root/deploy/linux/patches/ao-supervised-codex.patch"
git -C "$ao_source" diff --quiet
if [[ -n "$(git -C "$ao_source" ls-files --others --exclude-standard)" ]]; then
  echo "AO source checkout must be clean before applying the pinned patch" >&2
  exit 1
fi
git -C "$ao_source" apply --check "$patch"
git -C "$ao_source" apply "$patch"
echo "Applied Cylo supervised Codex container patch to AO $expected"
