#!/usr/bin/env bash
set -euo pipefail

image=${1:-cylo-ao-opencode:1.18.33}
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

docker build \
  --pull=false \
  --build-arg OPENCODE_VERSION=1.18.33 \
  --tag "$image" \
  "$script_dir"
