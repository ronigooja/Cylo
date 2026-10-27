#!/usr/bin/env bash
set -euo pipefail

release_dir="${1:-}"
if [[ -z "$release_dir" || $# -ne 1 ]]; then
  echo "usage: $0 IMMUTABLE_RELEASE_DIRECTORY" >&2
  exit 2
fi
release_dir="$(realpath -e -- "$release_dir")"
if [[ ! -f "$release_dir/bin/zoe" || ! -x "$release_dir/bin/zoe" ]]; then
  echo "release must contain an executable bin/zoe" >&2
  exit 1
fi
bin_dir="${ZOE_BIN_DIR:-/usr/local/bin}"
install -d -m 0755 -- "$bin_dir"
if [[ -e "$bin_dir/zoe" && ! -L "$bin_dir/zoe" ]]; then
  echo "refusing to replace a non-symlink zoe executable" >&2
  exit 1
fi
stage="$(mktemp -d "$bin_dir/.zoe-install.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
ln -s -- "$release_dir/bin/zoe" "$stage/zoe"
mv -Tf -- "$stage/zoe" "$bin_dir/zoe"
printf 'Installed %s/zoe -> %s/bin/zoe\n' "$bin_dir" "$release_dir"
