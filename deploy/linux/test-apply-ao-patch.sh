#!/usr/bin/env bash
set -euo pipefail

expected="7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff"
expected_tree="d46d2869a126e9874e74d4efef98c4f9ab32f011"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
apply_script="$repo_root/deploy/linux/apply-ao-patch.sh"
ao_source="${1:-}"
if [[ -z "$ao_source" || ! -e "$ao_source/.git" ]]; then
  echo "usage: $0 /path/to/agent-orchestrator-checkout" >&2
  exit 2
fi
if ! git -C "$ao_source" cat-file -e "$expected^{commit}" 2>/dev/null; then
  echo "AO source repository does not contain the pinned base commit $expected" >&2
  exit 1
fi

test_root="$(mktemp -d "${TMPDIR:-/tmp}/cylo-test-ao-patch.XXXXXX")"
worktrees=()
cleanup() {
  local path
  for path in "${worktrees[@]}"; do
    git -C "$ao_source" worktree remove --force "$path" >/dev/null 2>&1 || true
  done
  rm -rf -- "$test_root"
}
trap cleanup EXIT

fixture() {
  local name="$1"
  local revision="$2"
  local result_name="$3"
  local path="$test_root/$name"
  git -C "$ao_source" worktree add --quiet --detach "$path" "$revision"
  worktrees+=("$path")
  printf -v "$result_name" '%s' "$path"
}

expect_rejected() {
  local name="$1"
  local path="$2"
  local expected_message="$3"
  local output="$test_root/$name.log"
  if "$apply_script" "$path" >"$output" 2>&1; then
    cat "$output" >&2
    echo "$name fixture was accepted" >&2
    exit 1
  fi
  if ! grep -Fq "$expected_message" "$output"; then
    cat "$output" >&2
    echo "$name fixture failed for an unexpected reason" >&2
    exit 1
  fi
}

fixture clean "$expected" clean_path
"$apply_script" "$clean_path"
git -C "$clean_path" add -A
actual_tree="$(git -C "$clean_path" write-tree)"
if [[ "$actual_tree" != "$expected_tree" ]]; then
  echo "clean apply tree mismatch: expected $expected_tree, found $actual_tree" >&2
  exit 1
fi

fixture wrong-base "$expected^1" wrong_base_path
expect_rejected wrong-base "$wrong_base_path" "AO source revision mismatch"

fixture dirty "$expected" dirty_path
printf '\n# synthetic unstaged apply-test dirt\n' >>"$dirty_path/backend/go.mod"
expect_rejected dirty "$dirty_path" "AO source checkout must be clean"

fixture staged-only "$expected" staged_path
printf '\n# synthetic index-only apply-test dirt\n' >>"$staged_path/backend/go.mod"
git -C "$staged_path" add backend/go.mod
if ! git -C "$staged_path" diff --quiet; then
  echo "staged-only fixture has worktree dirt" >&2
  exit 1
fi
if git -C "$staged_path" diff --cached --quiet; then
  echo "staged-only fixture did not isolate index dirt" >&2
  exit 1
fi
expect_rejected staged-only "$staged_path" "AO source checkout must be clean"

fixture untracked "$expected" untracked_path
touch "$untracked_path/synthetic-untracked-apply-test-file"
expect_rejected untracked "$untracked_path" "AO source checkout must be clean"

echo "AO patch application checks passed for clean, wrong-base, dirty, staged-only, and untracked fixtures"
