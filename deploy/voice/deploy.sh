#!/usr/bin/env bash
set -euo pipefail
umask 077

readonly gateway_repository=${VOICE_GATEWAY_REPOSITORY:-https://github.com/ronigooja/chatgpt-web-voice.git}
readonly upstream_revision=${VOICE_GATEWAY_REVISION:-main}
readonly upstream_dir=/opt/zoe/voice/upstream
readonly config_dir=/etc/zoe
readonly data_dir=/var/lib/zoe/voice
readonly compose_file=/etc/zoe/voice.compose.yaml
readonly env_file=/etc/zoe/voice.env
readonly admin_file=/etc/zoe/voice-admin.txt
readonly lan_env_file=/etc/zoe/voice.lan.env
readonly lan_compose_file=/etc/zoe/voice.lan.compose.yaml
readonly script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run this deployment as root." >&2
  exit 1
fi

install -d -m 0755 /opt/zoe/voice "$config_dir"
install -d -o 1000 -g 1000 -m 0700 "$data_dir"

if [[ ! -d "$upstream_dir/.git" ]]; then
  (umask 022; git clone "$gateway_repository" "$upstream_dir")
fi
git -C "$upstream_dir" fetch --depth 1 origin "$upstream_revision"
git -C "$upstream_dir" checkout --detach FETCH_HEAD
resolved_revision="$(git -C "$upstream_dir" rev-parse HEAD)"
if [[ -z "$resolved_revision" ]]; then
  echo "Voice gateway revision could not be resolved." >&2
  exit 1
fi
# The process runs as UID 1000; make public static assets readable even when
# this script was first run under a restrictive secret-file umask.
chmod -R a+rX "$upstream_dir/static"

if [[ ! -e "$env_file" ]]; then
  admin_password="$(openssl rand -base64 36 | tr -d '\n')"
  encryption_key="$(openssl rand -hex 32)"
  cat >"$env_file" <<EOF
VOICE_AUTH_USERNAME=zoe-admin
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
    printf 'Username: %s\n' 'zoe-admin'
    printf 'Password: %s\n' "$admin_password"
  } >"$admin_file"
  chmod 0600 "$admin_file"
  unset admin_password encryption_key
elif [[ ! -e "$admin_file" ]]; then
  echo "$env_file already exists without the matching admin secret file; refusing to replace deployment credentials." >&2
  exit 1
fi

install -m 0644 "$script_dir/compose.yaml" "$compose_file"
if ! docker network inspect zoe >/dev/null 2>&1; then
  docker network create zoe >/dev/null
fi

lan_bind=""
if [[ "${VOICE_LAN_BIND+x}" == x ]]; then
  lan_bind="$VOICE_LAN_BIND"
elif [[ -e "$lan_env_file" ]]; then
  lan_bind="$(sed -n 's/^VOICE_LAN_BIND=//p' "$lan_env_file")"
fi
if [[ "${VOICE_LAN_BIND+x}" == x && -z "$lan_bind" ]]; then
  : >"$lan_env_file"
  chmod 0600 "$lan_env_file"
fi
compose_args=(--project-name zoe-voice --file "$compose_file")
if [[ -n "$lan_bind" ]]; then
  python3 -c 'import ipaddress,sys; ip=ipaddress.IPv4Address(sys.argv[1]); sys.exit(0 if ip.is_private and not ip.is_loopback and not ip.is_link_local else 1)' "$lan_bind" || {
    echo "VOICE_LAN_BIND must be a private IPv4 address assigned to this host." >&2
    exit 1
  }
  if ! ip -o -4 addr show | awk '{split($4,a,"/"); print a[1]}' | grep -Fxq "$lan_bind"; then
    echo "VOICE_LAN_BIND is not assigned to a host network interface." >&2
    exit 1
  fi
  printf 'VOICE_LAN_BIND=%s\n' "$lan_bind" >"$lan_env_file"
  chmod 0600 "$lan_env_file"
  install -m 0644 "$script_dir/compose.lan.yaml" "$lan_compose_file"
  compose_args+=(--env-file "$lan_env_file" --file "$lan_compose_file")
fi

docker compose "${compose_args[@]}" up --detach --build
python3 "$script_dir/provision-api-key.py"
echo "Voice gateway deployed from $upstream_revision."
echo "Management URL: http://127.0.0.1:18090/login"
echo "Management credentials file: $admin_file (root-readable only)"
if [[ -n "$lan_bind" ]]; then
  echo "LAN management URL: http://$lan_bind:18090/login"
fi
