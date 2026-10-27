# Deployment operations

This runbook applies to the current private, single-instance Cylo deployment. It is an operator procedure; completing a command is not product acceptance. The [master plan](../master-plan.md) defines architecture decisions and open acceptance boundaries.

## Components and ownership

- `cylo-compose.service` supervises the Go server, memory worker, and Qdrant.
- `cylo-ao.service` runs the Cylo-patched AO daemon. AO owns creation and lifecycle of runtime containers.
- `cylo-runner.service` is the independent execution runner.
- `cylo-macair-tunnel.service` maintains private SSH forwarding to the Mac client.
- `/etc/cylo` contains deployment configuration and secret references; `/var/lib/cylo` contains authority data and persistent service data.
- The existing remote CLIProxyAPI deployment and the Space3044 voice gateway are external dependencies. Cylo does not change their configuration or lifecycle.

The source deployment layout is in [`deploy/linux`](../../deploy/linux/). Do not copy credentials, installation links, or environment-file contents into logs, tickets, or chat.

## Health and address checks

Run these checks on the deployment host without printing secret files:

```sh
systemctl is-active cylo-compose cylo-ao cylo-runner cylo-macair-tunnel
curl --fail http://127.0.0.1:8787/healthz
curl --fail http://127.0.0.1:18001/readyz
```

The LAN binding resolves `debian.local` when `cylo-compose` starts, using [`resolve-server-address.py`](../../deploy/linux/resolve-server-address.py). If the host address changes, restart the service so Docker receives the new numeric address. Health/readiness proves process reachability only; it does not prove model execution, memory processing, voice operation, or task completion.

## Fresh instance and key rotation

The current authentication baseline is one shared instance key. There is no owner password, device approval flow, device-session login, or fixed client endpoint in the current source. Use the operator CLI to initialize or rotate keys while the server is stopped:

```sh
cylo key init --database PATH --output NEW_PRIVATE_JSON --server URL
cylo key rotate --database PATH --output NEW_PRIVATE_JSON --server URL
```

Use a new private destination for each output, update the deployment secret reference, and keep the generated key and `cylo://connect` link private. Apple clients import the server URL and key and store the credential in Keychain. A successful key command is source-level setup evidence, not multi-device or live-login acceptance.

For logical export, use a new destination and a reviewed key file:

```sh
python3 tools/cylo_export.py --server URL --key-file PRIVATE_JSON --output NEW_ZIP
```

The export is bounded to 1 GiB, refuses redirects and overwrites, and omits runtime volumes, secrets, and backups. Destruction uses [`tools/cylo_destroy.py`](../../tools/cylo_destroy.py) with a manifest and receipt; execution is path-limited, requires an explicit instance confirmation, and retains owned files unless external cleanup proof passes. No real external cleanup adapter is shipped.

## Unknown AO create outcomes

Never retry an unknown AO create as a new create. Stop `cylo-runner`, drain the original AO connections, and run reconciliation from the reviewed runner/AO build with complete AO and Docker inventories. The reconciliation command records either an immutable no-effect receipt or a refusal; a refusal leaves the run uncertain and its capacity slot occupied. Restart the runner after maintenance. Follow the detailed procedure in [local deployment history](../local-deployment.md#unknown-ao-creation-reconciliation), which remains the source-linked operational record.

## Voice gateway

The selected gateway is [Space3044/chatgpt-web-voice](https://github.com/Space3044/chatgpt-web-voice). Account import is manual. Gateway reachability or a successful management login does not establish Cylo voice acceptance; trusted transport, account/media leases, interruption, and deletion boundaries still require the acceptance scenarios.

## What this runbook does not establish

A deployment check does not establish end-to-end chat, Apple installation, push delivery, model generation, voice media, backup restore, or production recovery. Record those results in an evidence entry only after the corresponding acceptance procedure has been run.
