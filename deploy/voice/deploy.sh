#!/usr/bin/env bash
set -euo pipefail
umask 077

readonly upstream_revision=6338ac478070aaa34bfe7a3c0469caf9e9174c02
readonly upstream_dir=/opt/cylo/voice/upstream
readonly config_dir=/etc/cylo
readonly data_dir=/var/lib/cylo/voice
readonly compose_file=/etc/cylo/voice.compose.yaml
readonly env_file=/etc/cylo/voice.env
readonly admin_file=/etc/cylo/voice-admin.txt
readonly script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run this deployment as root." >&2
  exit 1
fi

install -d -m 0755 /opt/cylo/voice "$config_dir"
install -d -o 1000 -g 1000 -m 0700 "$data_dir"

if [[ ! -d "$upstream_dir/.git" ]]; then
  (umask 022; git clone https://github.com/Space3044/chatgpt-web-voice.git "$upstream_dir")
fi
git -C "$upstream_dir" fetch --depth 1 origin "$upstream_revision"
git -C "$upstream_dir" checkout --detach "$upstream_revision"
if [[ "$(git -C "$upstream_dir" rev-parse HEAD)" != "$upstream_revision" ]]; then
  echo "Upstream revision did not match the pinned commit." >&2
  exit 1
fi
# The process runs as UID 1000; make public static assets readable even when
# this script was first run under a restrictive secret-file umask.
chmod -R a+rX "$upstream_dir/static"

if [[ ! -e "$env_file" ]]; then
  admin_password="$(openssl rand -base64 36 | tr -d '\n')"
  encryption_key="$(openssl rand -hex 32)"
  cat >"$env_file" <<EOF
VOICE_AUTH_USERNAME=cylo-admin
VOICE_AUTH_PASSWORD=$admin_password
VOICE_TOKEN_ENCRYPTION_KEY=$encryption_key
VOICE_ENV=production
VOICE_LISTEN_ADDR=0.0.0.0:8090
VOICE_TLS=false
VOICE_SKIP_SSL_VERIFY=false
VOICE_DATA_DIR=/app/data
VOICE_DATABASE_FILE=/app/data/voice.db
VOICE_LOG_FORMAT=json
VOICE_LOG_LEVEL=info
VOICE_UPSTREAM_TRANSPORT=curl-impersonate
VOICE_IMPERSONATE=edge_101
VOICE_CURL_IMPERSONATE_BIN=/app/bin/curl-impersonate/curl_edge101
EOF
  chmod 0600 "$env_file"
  {
    printf '%s\n' 'Voice gateway management login (keep this file private):'
    printf 'Username: %s\n' 'cylo-admin'
    printf 'Password: %s\n' "$admin_password"
  } >"$admin_file"
  chmod 0600 "$admin_file"
  unset admin_password encryption_key
elif [[ ! -e "$admin_file" ]]; then
  echo "$env_file already exists without the matching admin secret file; refusing to replace deployment credentials." >&2
  exit 1
fi

install -m 0644 "$script_dir/compose.yaml" "$compose_file"
if ! docker network inspect cylo >/dev/null 2>&1; then
  docker network create cylo >/dev/null
fi

docker compose --project-name cylo-voice --file "$compose_file" up --detach --build
python3 "$script_dir/provision-api-key.py"
echo "Voice gateway deployed from $upstream_revision."
echo "Management URL: http://127.0.0.1:18090/login"
echo "Management credentials file: $admin_file (root-readable only)"
