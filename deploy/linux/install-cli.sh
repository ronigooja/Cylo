#!/usr/bin/env bash
set -euo pipefail

release_dir="${1:-}"
if [[ -z "$release_dir" || $# -ne 1 ]]; then
  echo "usage: $0 IMMUTABLE_RELEASE_DIRECTORY" >&2
  exit 2
fi
release_dir="$(realpath -e -- "$release_dir")"
if [[ ! -f "$release_dir/bin/cylo" || ! -x "$release_dir/bin/cylo" ]]; then
  echo "release must contain an executable bin/cylo" >&2
  exit 1
fi
bin_dir="${CYLO_BIN_DIR:-/usr/local/bin}"
install -d -m 0755 -- "$bin_dir"
if [[ -e "$bin_dir/cylo" && ! -L "$bin_dir/cylo" ]]; then
  echo "refusing to replace a non-symlink cylo executable" >&2
  exit 1
fi
stage="$(mktemp -d "$bin_dir/.cylo-install.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
ln -s -- "$release_dir/bin/cylo" "$stage/cylo"
mv -Tf -- "$stage/cylo" "$bin_dir/cylo"
printf 'Installed %s/cylo -> %s/bin/cylo\n' "$bin_dir" "$release_dir"
