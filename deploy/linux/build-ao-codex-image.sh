#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
image="${AO_CODEX_CONTAINER_IMAGE:-zoe-ao-codex:0.146.0}"
docker_bin="${ZOE_DOCKER_BINARY:-docker}"
"$docker_bin" build --pull -t "$image" "$repo_root/deploy/linux/AO/runtime"
printf 'Built supervised AO Codex image: %s\n' "$image"
