#!/usr/bin/env bash
set -euo pipefail

image=${1:-zoe-opencode:1.18.33}
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

docker build \
  --pull=false \
  --tag "$image" \
  "$script_dir"
