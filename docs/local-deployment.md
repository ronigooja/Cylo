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

The control server binds host loopback port 8787. AO binds host loopback port
18001; its listener uses the trusted local-process boundary and does not
enforce bearer authentication. Health/readiness responses do not prove model
execution, memory processing, or task completion.

## macOS application

The app is installed at
`/Users/zhuwei/terminal/cylo-native-install-20261024/Cylo.app`.
All Mac deployment artifacts remain below that dedicated directory. The app
uses `http://127.0.0.1:8787`, forwarded over SSH to the Linux server. This URL
depends on the SSH tunnel and does not expose the server on the LAN.

The first device must register through **More > Settings** in the app. Enter a device
name and the deployment-provided one-use bootstrap code, then select
**Bootstrap first approved device**. A temporary private code file is supplied
at `bootstrap-code.tmp` within the dedicated install directory. Remove that
temporary copy after successful registration. Do not paste the code into
chat or repository files. The app stores its device identity in Keychain.

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
