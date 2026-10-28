#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tag="${1:-}"
if [[ ! "$tag" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$ ]]; then
  echo "usage: $0 RELEASE_TAG" >&2
  exit 2
fi
if [[ ! -d "$repo_root/cmd/zoe-runner" ]]; then
  echo "cmd/zoe-runner is not present in this source revision; integrate the coordinated runtime source before building the Linux release" >&2
  exit 1
fi

cd "$repo_root"

build_root="${ZOE_BUILD_ROOT:-/var/cache/zoe/builds}"
release_root="${ZOE_RELEASE_ROOT:-/opt/zoe/releases}"
docker_bin="${ZOE_DOCKER_BINARY:-docker}"
go_bin="${GO:-go}"
build_goarch="${ZOE_BUILD_GOARCH:-$("$go_bin" env GOARCH)}"
build_platform="${ZOE_BUILD_PLATFORM:-linux/$build_goarch}"
case "$build_goarch" in
  amd64|arm64) ;;
  *) echo "unsupported release GOARCH: $build_goarch" >&2; exit 1 ;;
esac
if [[ "$build_platform" != "linux/$build_goarch" ]]; then
  echo "ZOE_BUILD_PLATFORM must match the release binaries' linux/$build_goarch architecture" >&2
  exit 1
fi
release_dir="$release_root/$tag"
if [[ -e "$release_dir" || -L "$release_dir" ]]; then
  echo "release directory already exists; choose a new immutable release tag" >&2
  exit 1
fi
mkdir -p "$build_root" "$release_root"
cache_dir="${ZOE_BUILDX_CACHE_DIR:-$build_root/buildkit-cache}"
mkdir -p "$cache_dir"
buildx_cache_args=()
if [[ -n "${ZOE_BUILDX_CACHE_FROM:-}" ]]; then buildx_cache_args+=(--cache-from "$ZOE_BUILDX_CACHE_FROM"); fi
if [[ -n "${ZOE_BUILDX_CACHE_TO:-}" ]]; then buildx_cache_args+=(--cache-to "$ZOE_BUILDX_CACHE_TO"); fi
# Bash with nounset rejects expanding an empty array; use a single command array.
buildx_base=(buildx build --load --platform "$build_platform")
buildx_base+=("${buildx_cache_args[@]}")
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
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/zoe" ./cmd/zoe
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/zoe-server" ./cmd/zoe-server
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/zoe-runner" ./cmd/zoe-runner
"$go_bin" build -trimpath -buildvcs=false -ldflags='-s -w' -o "$build_dir/bin/zoe-secret-entrypoint" ./deploy/linux/secret-entrypoint.go
install -m 0755 "$build_dir/bin/zoe" "$release_stage/bin/zoe"
install -m 0755 "$build_dir/bin/zoe-server" "$release_stage/bin/zoe-server"
install -m 0755 "$build_dir/bin/zoe-runner" "$release_stage/bin/zoe-runner"
install -m 0755 "$build_dir/bin/zoe-secret-entrypoint" "$release_stage/bin/zoe-secret-entrypoint"
cp -a "$repo_root/deploy/linux" "$release_stage/deploy-linux"

"$docker_bin" "${buildx_base[@]}" -f "$repo_root/deploy/linux/Dockerfile.server" -t "zoe-server:$tag" "$repo_root"
"$docker_bin" "${buildx_base[@]}" -f "$repo_root/deploy/linux/Dockerfile.memory" -t "zoe-memory-worker:$tag" "$repo_root"
"$docker_bin" "${buildx_base[@]}" -f "$repo_root/deploy/linux/Dockerfile.runner" -t "zoe-runner:$tag" "$repo_root"

# Record immutable source, binary checksums, and local image IDs before publishing.
source_revision="$(git rev-parse HEAD)"
(
  cd "$release_stage"
  sha256sum bin/* > SHA256SUMS
  cat > release-manifest.json <<EOF
{
  "schema": 1,
  "release": "${tag}",
  "source_revision": "${source_revision}",
  "platform": "${build_platform}",
  "images": {
    "server": "$($docker_bin image inspect "zoe-server:$tag" --format '{{.Id}}')",
    "memory_worker": "$($docker_bin image inspect "zoe-memory-worker:$tag" --format '{{.Id}}')",
    "runner": "$($docker_bin image inspect "zoe-runner:$tag" --format '{{.Id}}')"
  }
}
EOF
)

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
printf 'Built Linux release %s (%s) under %s and images zoe-server:%s, zoe-memory-worker:%s, zoe-runner:%s\n' "$tag" "$build_platform" "$release_dir" "$tag" "$tag" "$tag"
printf 'Build scratch was private under %s and removed; authoritative and runner state remain under /var/lib/zoe.\n' "$build_root"
