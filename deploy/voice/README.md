# Space3044 voice gateway deployment

This deployment runs Space3044 `chatgpt-web-voice` at pinned commit
`6338ac478070aaa34bfe7a3c0469caf9e9174c02` (MIT). It is a separate Go
gateway container, not a Cylo implementation of voice account leasing or
media termination.

## Host deployment

Run `sudo deploy/voice/deploy.sh` from a Cylo checkout. The script checks out
the fixed upstream commit under `/opt/cylo/voice/upstream`, creates the
gateway's management password and AES encryption key if this is a first
install, builds the upstream image, and starts the container with Docker's
`unless-stopped` restart policy. It never imports a ChatGPT account. The
database persists under `/var/lib/cylo/voice`; deployment configuration and
generated credentials are root-readable only under `/etc/cylo`.
An initial downstream key named `cylo-control-plane` is created once and
stored in `/etc/cylo/voice-api-key.env`; later deployments reuse this file.

By default, the management page is bound only to host loopback:

```text
http://127.0.0.1:18090/login
```

Open this URL from a browser that can reach the Linux host's loopback address
(for example, through an operator-managed port forward). Read the generated
management login from `/etc/cylo/voice-admin.txt` on the Linux host. Keep that
file there; do not paste it into chat, tickets, or source files. On the
management UI, add only a personally owned ChatGPT Web account that the user
is authorized to use and that has Web Voice access. This account's
`access_token` must be entered manually; it is not an OpenAI API key, is
encrypted in the gateway database, and does not refresh automatically.

The gateway uses plain HTTP on loopback and on the private Docker network named
`cylo`. A LAN bind is an explicit opt-in for an address assigned to the host:

```bash
sudo env VOICE_LAN_BIND=192.168.1.4 ./deploy/voice/deploy.sh
```

The script persists this bind address at `/etc/cylo/voice.lan.env` and adds a
second host mapping while retaining the loopback mapping. It accepts only a
private IPv4 host address; it does not bind all interfaces. Without this
setting, port 18090 stays loopback-only. The LAN page uses HTTP, so only use it
on a trusted private network. To return to loopback-only, redeploy with
`sudo env VOICE_LAN_BIND= ./deploy/voice/deploy.sh`. The container has a
384 MiB memory cap, 1 CPU cap, 128 PID cap, a read-only root filesystem, and
bounded local Docker logs. Persistent gateway data is stored in
`/var/lib/cylo/voice`.

## Cylo integration boundary

Inside the shared Docker network, the gateway URL is
`http://voice-gateway:8090`; the downstream API base routes include
`GET /v1/voice/config`, `POST /v1/voice/sessions`,
`POST /v1/voice/sessions/{id}/context`, and
`DELETE /v1/voice/sessions/{id}`. Downstream access uses a separately created
Bearer API key (`vgw_live_…`) and cannot access the management pages or account
pool. The key is shown only once when created and must be stored in a
root-readable secret file.

This gateway does not provide trusted HTTPS for Cylo clients. Cylo voice must
remain disabled until an approved HTTPS path and account/session lifecycle
integration are in place. In particular, gateway DELETE releases its own
binding but is not evidence that upstream WebRTC media ended. The account-media
lease feature therefore remains disabled, and deployment alone does not prove
that voice calls or task dispatch work.
