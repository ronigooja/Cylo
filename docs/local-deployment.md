# Local deployment operations

This runbook describes the local Linux installation and the macOS client on
`zhuwei@macair`. Deployment evidence and acceptance limits belong in the
[master plan](master-plan.md). Starting services is not end-to-end product
acceptance.

## Linux services and storage

- `/opt/cylo/releases/current` selects the immutable server/runner release.
- `cylo-compose.service` supervises the server, memory worker, and Qdrant.
- `cylo-ao.service` runs the separately managed, Cylo-patched AO daemon.
- `cylo-runner.service` runs the trusted execution runner.
- `cylo-macair-tunnel.service` maintains private SSH forwarding to the Mac.
- `/etc/cylo` contains deployment configuration and secret references.
- `/var/lib/cylo` contains authority databases and persistent service data.

The external Docker network `cylo` is prepared by
`deploy/linux/prepare-host-layout.sh`. The existing remote CLIProxyAPI
deployment remains outside Cylo's lifecycle. Preserve the existing runner
deployment-authority database and capability containers during maintenance.

Check service status without printing environment files or secret values:

```bash
systemctl is-active cylo-compose cylo-ao cylo-runner cylo-macair-tunnel
curl --fail http://127.0.0.1:8787/healthz
curl --fail http://127.0.0.1:18001/readyz
```

The control server binds port 8787 on both host loopback and the authorized
LAN interface. The LAN endpoint is `http://debian.local:8787`.
The deployment override `/etc/cylo/control.lan.compose.yaml` adds this binding;
`/etc/systemd/system/cylo-compose.service.d/lan.conf` includes the override in
the service's Compose commands. The source template is
`deploy/linux/docker-compose.lan.yml`; set `CYLO_SERVER_LAN_ADDRESS` to the
host LAN address. Update that setting if the host address changes.
AO binds host loopback port
18001; its listener uses the trusted local-process boundary and does not
enforce bearer authentication. Health/readiness responses do not prove model
execution, memory processing, or task completion.

## macOS application

### Password-login replacement: rollout boundary

The approved replacement uses the fixed endpoint `http://debian.local:8787`,
one-use owner setup with a password, and password login on additional devices.
The address is not editable. Password login does not require another device to
be online. This source change does not update the running server or installed
apps; the earlier installed-device procedure below describes that installation
until rollout is performed and verified.

An existing installation whose bootstrap code was already consumed requires a
fresh setup code from a server administrator. After stopping the server, run
the replacement server binary on its host:

```sh
cylo-server recover-owner --database /path/to/cylo.db --confirm-revoke-all-devices
```

The operation requires the exclusive database lock, preserves the owner identity
and business data, clears the password for setup, revokes device sessions, and
requests active runs to stop. Keep the generated code private and enter it in
the replacement app's first-setup form with the new owner password. The stored
recovery code takes precedence over the original deployment bootstrap code;
do not paste either code or the password into chat or repository files. A stop
request is not termination proof. Back up authoritative data before rollout;
run this command only as an intentional migration or recovery operation.

The fixed endpoint currently uses plain HTTP on the trusted LAN. Password and
session transport protection remains an acceptance gap; this source change
does not establish HTTPS or modify the deployed network configuration.

### Earlier installed client

The app is installed at `/Applications/Cylo.app`. Current update artifacts and
logs are under `~/Developer/Cylo/runs/fixed-server-ui-20261025/`. The app
can use `http://debian.local:8787` directly on the LAN. The existing
`http://127.0.0.1:8787` endpoint remains available through the SSH tunnel.
The LAN URL uses plain HTTP and is intended for the trusted local network.

The first device must register through **More > Settings** in the app. Enter a device
name and the deployment-provided one-use bootstrap code, then select
**Bootstrap first approved device**. Use a current deployment-provided code;
remove any temporary private copy after registration. Do not paste the code
into chat or repository files. Additional devices request approval from an
already approved device.

The updated client fixes the server address to `http://debian.local:8787` and
defaults to Keychain, preserving any existing explicit storage choice for that
origin. There is no address field or mandatory storage picker. Storage controls
are available under **Settings > Advanced settings**. Keychain may request system
permission. Local files use private directories (0700) and files (0600) below
`~/Library/Application Support/com.cylo.mac/DeviceIdentities/<origin-hash>/`.
Local file credentials are not encrypted by Keychain.

Changing storage does not automatically copy or overwrite an identity. Use the
explicit copy-and-switch action to preserve an existing approved identity; this
may require allowing the old Keychain record to be read once. Switching without
copying uses a separate identity and requires device approval. Denied Keychain
access does not erase the existing credential.

Remote automation cannot bypass macOS Keychain access controls or
Accessibility permissions. An installed/running app is not evidence of
successful device registration. The local app uses ad-hoc signing; APNs and
distribution signing require separate configuration and acceptance.

## Voice gateway

The separately deployed gateway is
[Space3044/chatgpt-web-voice](https://github.com/Space3044/chatgpt-web-voice),
pinned to `6338ac478070aaa34bfe7a3c0469caf9e9174c02`.
See [the deployment instructions](../deploy/voice/README.md).

- Linux management URL: `http://127.0.0.1:18090/login`.
- Authorized LAN management URL: `http://debian.local:18090/login`.
- Source: `/opt/cylo/voice/upstream`.
- Container: `cylo-voice-gateway`.
- Persistent data: `/var/lib/cylo/voice`.
- Compose configuration: `/etc/cylo/voice.compose.yaml`.
- Private environment: `/etc/cylo/voice.env`.
- Management login reference: `/etc/cylo/voice-admin.txt`, on Linux only.

The local installation explicitly publishes the gateway on `192.168.1.4`
as well as loopback, following the user's request for `debian.local` access.
The opt-in address is recorded in `/etc/cylo/voice.lan.env`; the repository
default remains loopback only. The SSH tunnel also forwards Mac loopback
port 18090. A browser worker in a
container has its own loopback interface; it needs an explicitly configured
network route before it can use the gateway. Do not assume the host's
`127.0.0.1` URL works inside a container.

The user must manually import a ChatGPT Web account in the gateway management
UI. Deploying the gateway does not establish voice functionality in Cylo:
trusted HTTPS and account/media lifecycle integration remain required. Keep
Cylo account-media disabled until those acceptance requirements are met.
