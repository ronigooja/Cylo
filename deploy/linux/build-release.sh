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

cd "$repo_root"

build_root="${CYLO_BUILD_ROOT:-/var/cache/cylo/builds}"
release_root="${CYLO_RELEASE_ROOT:-/opt/cylo/releases}"
docker_bin="${CYLO_DOCKER_BINARY:-docker}"
go_bin="${GO:-go}"
build_goarch="${CYLO_BUILD_GOARCH:-$("$go_bin" env GOARCH)}"
build_platform="${CYLO_BUILD_PLATFORM:-linux/$build_goarch}"
case "$build_goarch" in
  amd64|arm64) ;;
  *) echo "unsupported release GOARCH: $build_goarch" >&2; exit 1 ;;
esac
if [[ "$build_platform" != "linux/$build_goarch" ]]; then
  echo "CYLO_BUILD_PLATFORM must match the release binaries' linux/$build_goarch architecture" >&2
  exit 1
fi
release_dir="$release_root/$tag"
if [[ -e "$release_dir" || -L "$release_dir" ]]; then
  echo "release directory already exists; choose a new immutable release tag" >&2
  exit 1
fi
mkdir -p "$build_root" "$release_root"
build_dir=""
release_stage=""
cleanup_stages() {
  [[ -z "$release_stage" ]] || rm -rf -- "$release_stage"
  [[ -z "$build_dir" ]] || rm -rf -- "$build_dir"
}
trap cleanup_stages EXIT
build_dir="$(mktemp -d "$build_root/.${tag}.build.XXXXXX")"
release_stage="$(mktemp -d "$release_root/.${tag}.staging.XXXXXX")"
chmod 0700 "$build_dir"
mkdir -p "$build_dir/bin" "$release_stage/bin"
chmod 0755 "$release_stage"

export CGO_ENABLED=0 GOOS=linux GOARCH="$build_goarch"
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/cylo" ./cmd/cylo
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/cylo-server" ./cmd/cylo-server
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/cylo-runner" ./cmd/cylo-runner
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/cylo-secret-entrypoint" ./deploy/linux/secret-entrypoint.go
install -m 0755 "$build_dir/bin/cylo" "$release_stage/bin/cylo"
install -m 0755 "$build_dir/bin/cylo-runner" "$release_stage/bin/cylo-runner"
install -m 0755 "$build_dir/bin/cylo-secret-entrypoint" "$release_stage/bin/cylo-secret-entrypoint"
cp -a "$repo_root/deploy/linux" "$release_stage/deploy-linux"

"$docker_bin" buildx build --load --platform "$build_platform" -f "$repo_root/deploy/linux/Dockerfile.server" -t "cylo-server:$tag" "$repo_root"
"$docker_bin" buildx build --load --platform "$build_platform" -f "$repo_root/deploy/linux/Dockerfile.memory" -t "cylo-memory-worker:$tag" "$repo_root"
"$docker_bin" buildx build --load --platform "$build_platform" -f "$repo_root/deploy/linux/Dockerfile.runner" -t "cylo-runner:$tag" "$repo_root"

# Linux GNU mv -Tn publishes the directory as one no-clobber rename. The image
# tags above are separate Docker operations and are not a multi-tag transaction.
if ! mv -Tn -- "$release_stage" "$release_dir"; then
  echo "could not publish release directory; choose a new immutable release tag" >&2
  exit 1
fi
if [[ -e "$release_stage" || -L "$release_stage" ]]; then
  echo "another build published this release tag first; this build's release was not published" >&2
  exit 1
fi
release_stage=""

printf 'Built Linux release %s (%s) under %s and images cylo-server:%s, cylo-memory-worker:%s, cylo-runner:%s\n' "$tag" "$build_platform" "$release_dir" "$tag" "$tag" "$tag"
printf 'Build scratch was private under %s and removed; authoritative and runner state remain under /var/lib/cylo.\n' "$build_root"
