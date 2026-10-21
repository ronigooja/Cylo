#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "run as root to prepare the Cylo host layout" >&2
  exit 1
fi

if ! getent group docker >/dev/null; then
  echo "the Docker Engine docker group is required for the trusted runner" >&2
  exit 1
fi
if ! getent group cylo-runner >/dev/null; then
  groupadd --system cylo-runner
fi
if ! id cylo-runner >/dev/null 2>&1; then
  useradd --system --gid cylo-runner --home-dir /var/lib/cylo/runner --shell /usr/sbin/nologin cylo-runner
fi
usermod --append --groups docker cylo-runner

install -d -o root -g root -m 0755 /opt/cylo/releases
install -d -o root -g root -m 0750 /var/cache/cylo/builds
install -d -o root -g cylo-runner -m 0750 /var/lib/cylo
install -d -o 65532 -g 65532 -m 0700 /var/lib/cylo/server/data
install -d -o 65532 -g 65532 -m 0700 /var/lib/cylo/memory
install -d -o cylo-runner -g cylo-runner -m 0700 /var/lib/cylo/runner
install -d -o cylo-runner -g cylo-runner -m 0700 /var/lib/cylo/runner-authority
install -d -o root -g root -m 0700 /etc/cylo/secrets

authority=/var/lib/cylo/runner-authority/runner-deployment-authority.db
if [[ -L "$authority" ]]; then
  echo "runner deployment authority must not be a symlink" >&2
  exit 1
fi
if [[ ! -e "$authority" ]]; then
  install -o cylo-runner -g cylo-runner -m 0600 /dev/null "$authority"
fi
if [[ ! -f "$authority" ]]; then
  echo "runner deployment authority must be a regular file" >&2
  exit 1
fi
chown cylo-runner:cylo-runner "$authority"
chmod 0600 "$authority"

install -D -o root -g root -m 0644 "$(dirname "$0")/cylo-runner-tmpfiles.conf" /etc/tmpfiles.d/cylo-runner.conf
systemd-tmpfiles --create /etc/tmpfiles.d/cylo-runner.conf
systemctl daemon-reload

cat <<'EOF'
Host layout prepared. Supply secret files under /etc/cylo/secrets with mode 0400
and owner 65532:65532 for Compose services; runner token and AO token must also
be readable by cylo-runner. Copy the example environment files and set
deployment-specific paths, profiles, model routes, and existing external URLs.
EOF
