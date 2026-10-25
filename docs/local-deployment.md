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

### Installed owner-password release — 2026-10-25

The control server now uses `cylo-server:owner-login-f62b55f3a964`, built on a
GitHub-hosted ARM64 runner in [run 40231799160](https://github.com/ronigooja/Cylo/actions/runs/40231799160).
The macOS application at `/Applications/Cylo.app` was replaced with the verified
Apple CI artifact from revision `2d9d779`, signed ad hoc locally and launched.
Mac inputs, signing/install scripts, current artifacts, and logs are under
`~/Developer/Cylo/runs/owner-login-20261025/`. No local compilation or automated
suite was used for this rollout.

The fixed endpoint is `http://debian.local:8787`; there is no address input.
First setup requires a fresh one-use setup code and the new owner password.
Other devices subsequently sign in with the password without another device
being online. Device settings provide sign-out and remote revocation. Those
operations cancel that device's queued tasks and request active runs to stop;
a stop request does not prove termination.

This rollout migrated the existing database in place, preserving the owner
identity and business records while revoking all three legacy device credentials.
The host recovery operation was:

```sh
cylo-server recover-owner --database /var/lib/cylo/server/data/cylo.sqlite --confirm-revoke-all-devices
```

The command ran while the stack was stopped, under the authority-data UID with
its existing host parent-directory access group. The stored recovery code takes
precedence over the original deployment code. It was delivered to the Mac
clipboard without printing its value, and its temporary plaintext file was
deleted. The owner must paste it into the first-setup form and choose the
password; the agent did not set or retain a password. If the clipboard is lost
before setup, an administrator must intentionally stop the stack and repeat
recovery to obtain a fresh code. Never paste secrets into chat or repository files.

The user explicitly required no backups: none were created. Four old server
image tags/images and seven old Mac app/archive products in the earlier task
roots were removed. The active runner release and persistent data were retained.
The user deferred iOS installation/testing; this rollout's local iOS downloads,
staging bundle, and signing entitlements were removed, and the original combined
CI app artifact was deleted. The phone's existing application was not changed.

Keychain remains the default credential store, with an existing explicit storage
choice preserved. Advanced settings offer private-file storage and explicit
copying; switching does not silently move an identity. Private-file credentials
are not encrypted by Keychain. Remote automation cannot bypass macOS Keychain
or screenshot permissions. Signing verification, launch, process existence,
and an application window were confirmed; screenshot capture failed, and live
owner setup/password login remains unaccepted until performed by the owner.
The fixed plain-HTTP LAN endpoint still lacks protected password/session
transport; this rollout did not change network configuration or external services.

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
