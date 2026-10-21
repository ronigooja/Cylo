#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tag="${1:-}"
if [[ ! "$tag" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$ ]]; then
  echo "usage: $0 RELEASE_TAG" >&2
  exit 2
fi
if [[ ! -d "$repo_root/cmd/cylo-runner" ]]; then
  echo "cmd/cylo-runner is not present in this source revision; integrate the coordinated runtime source before building the Linux release" >&2
  exit 1
fi

build_root="${CYLO_BUILD_ROOT:-/var/cache/cylo/builds}"
release_root="${CYLO_RELEASE_ROOT:-/opt/cylo/releases}"
docker_bin="${CYLO_DOCKER_BINARY:-docker}"
release_dir="$release_root/$tag"
if [[ -e "$release_dir" || -L "$release_dir" ]]; then
  echo "release directory already exists; choose a new immutable release tag" >&2
  exit 1
fi
mkdir -p "$build_root/$tag/bin" "$release_dir/bin"
chmod 0700 "$build_root/$tag"

export CGO_ENABLED=0 GOOS=linux
"${GO:-go}" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_root/$tag/bin/cylo-server" ./cmd/cylo-server
"${GO:-go}" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_root/$tag/bin/cylo-runner" ./cmd/cylo-runner
"${GO:-go}" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_root/$tag/bin/cylo-secret-entrypoint" ./deploy/linux/secret-entrypoint.go
install -m 0755 "$build_root/$tag/bin/cylo-runner" "$release_dir/bin/cylo-runner"
install -m 0755 "$build_root/$tag/bin/cylo-secret-entrypoint" "$release_dir/bin/cylo-secret-entrypoint"
cp -a "$repo_root/deploy/linux" "$release_dir/deploy-linux"

"$docker_bin" buildx build --load --platform linux/amd64 -f "$repo_root/deploy/linux/Dockerfile.server" -t "cylo-server:$tag" "$repo_root"
"$docker_bin" buildx build --load --platform linux/amd64 -f "$repo_root/deploy/linux/Dockerfile.memory" -t "cylo-memory-worker:$tag" "$repo_root"
"$docker_bin" buildx build --load --platform linux/amd64 -f "$repo_root/deploy/linux/Dockerfile.runner" -t "cylo-runner:$tag" "$repo_root"

printf 'Built Linux release %s under %s and images cylo-server:%s, cylo-memory-worker:%s, cylo-runner:%s\n' "$tag" "$release_dir" "$tag" "$tag" "$tag"
printf 'Build outputs are private under %s; authoritative and runner state remain under /var/lib/cylo.\n' "$build_root/$tag"
