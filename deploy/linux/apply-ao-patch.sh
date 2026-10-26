#!/usr/bin/env bash
set -euo pipefail

expected="7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff"
expected_tree="eb2da2d0133907ee7669da53e7e7964b37c05ab8"
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
if ! git -C "$ao_source" diff --quiet; then
  echo "AO source checkout must be clean before applying the pinned patch" >&2
  exit 1
fi
if ! git -C "$ao_source" diff --cached --quiet; then
  echo "AO source checkout must be clean before applying the pinned patch" >&2
  exit 1
fi
if [[ -n "$(git -C "$ao_source" ls-files --others --exclude-standard)" ]]; then
  echo "AO source checkout must be clean before applying the pinned patch" >&2
  exit 1
fi
git -C "$ao_source" apply --check "$patch"
git -C "$ao_source" apply "$patch"
index_root="$(mktemp -d "${TMPDIR:-/tmp}/cylo-ao-patch-index.XXXXXX")"
cleanup_index() {
  rm -rf -- "$index_root"
}
trap cleanup_index EXIT
index_file="$index_root/index"
GIT_INDEX_FILE="$index_file" git -C "$ao_source" read-tree "$expected"
GIT_INDEX_FILE="$index_file" git -C "$ao_source" add -A
actual_tree="$(GIT_INDEX_FILE="$index_file" git -C "$ao_source" write-tree)"
if [[ "$actual_tree" != "$expected_tree" ]]; then
  echo "AO patch output tree mismatch: expected $expected_tree, found $actual_tree" >&2
  exit 1
fi
echo "Applied Cylo supervised Codex container patch to AO $expected"
