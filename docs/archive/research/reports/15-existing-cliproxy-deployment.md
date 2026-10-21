# Existing CLIProxyAPI Deployment and Cylo Reuse Plan

Reviewed: 2026-10-21. **The existing proxy is deployed and running; Cylo should reuse it rather than first installing another CLIProxyAPI instance.** The local Codex is configured to use a custom Responses provider. The user reports that login and use can both go through the proxy; this review did not retest login, model requests, or automatic account switching.

This report calls the existing remote deployment `existing-proxy`; it omits the public address, personal directory details, and service domain. See the separate [CLIProxyAPI and AO account-pool integration review](14-cliproxyapi-integration.md) for source capabilities and compatibility limits.

## Read-only evidence from this review

| Item | Finding |
|---|---|
| Connection | Connected using an already trusted SSH host key with `BatchMode=yes` and `ConnectTimeout=10`; host-key checking was not bypassed. |
| Deployment directory supplied by the user | Present; contains deployment scripts and notes, but is not a standalone CLIProxyAPI source checkout. The parent repository SHA is not evidence of the proxy version. |
| Runtime | The `CLIProxyAPI` process runs in a Docker container; the container is running, with restart policy `unless-stopped`. |
| Running image reference | `eceasy/cli-proxy-api:latest`. |
| Running image ID | `sha256:114ea95cadc393f679016285d5f8dc0b735126a25a018307c033e8c3b1b801d2`. This identifies local image contents; it is not a verified registry manifest digest. |
| Binary version / proxy source SHA | Not verified. `latest` does not identify a version, and the source SHA in research report 14 is not evidence of the deployed version. |
| Listen and publish addresses | The container listens on port 8317 on all container interfaces; the host publishes only loopback `8317/tcp`. This does not mean the host exposes that port directly to the public internet. |
| Routing configuration | `weighted-round-robin`; this does not establish the number of accounts or which account was selected at runtime. |
| Local Codex configuration | A custom provider is selected with `wire_api="responses"`; the endpoint is an HTTPS `/v1` URL. The domain, userinfo, query, and other configuration values were not printed. |
| Optional Codex provider flags | The selected provider does not explicitly configure `requires_openai_auth` or `supports_websockets`; absent fields were not treated as false. |
| Minimal reachability probe | One unauthenticated HTTP GET to `/` on the host's loopback-published port returned 200. The response body was not collected. This proves only that the root path is reachable, not model operation, authentication, pool operation, or overall health. |

Only deployment-directory metadata, process identity, Docker allowlisted fields, selected non-sensitive configuration fields, and local Codex provider fields were read. Full process arguments, complete configuration, environment variables, authentication files, keys, and proxy credentials were not printed; account-pool contents were not read. Nothing was installed, restarted, or changed on the remote host; no tokens were refreshed, no login was performed, and no model request was sent. The root-path request was not a billable model request.

The local configuration and remote container were checked separately. This review did not resolve private DNS, inspect the TLS endpoint, or trace a reverse proxy, so these facts alone do not prove that the current HTTPS endpoint ultimately routes to this container. That relationship comes from the deployment context supplied by the user and should be confirmed by the deployment owner during integration.

## Cylo integration path

This section describes the reviewed Codex route and does not constrain Cylo's agent scope. AO can orchestrate several coding-agent runtimes. Each runtime requires its own adapter for its model/provider, protocol, credentials, and recovery behavior. CLIProxyAPI is an optional route for compatible protocols; this review does not promise that every agent can connect to it directly. See [AO multi-agent and model-routing boundaries](16-ao-agent-provider-boundaries.md).

For the Codex route, retain `AO → native Codex CLI/app-server → existing-proxy Responses → Codex OAuth pool`. The existing local Codex provider is a reusable integration baseline. Separately align the binary, runtime user, `CODEX_HOME`, and launch environment used by AO. Keep app-server stdio RPC as AO's control protocol; only model requests should target the proxy's `/v1/responses` endpoint.

Before changing configuration, create an isolated provider configuration and a plan for injecting downstream access credentials for Cylo/AO, then check how AO's per-account shared `config.toml` links are affected. Do not copy OAuth credentials into Cylo, and do not let AO and the proxy both make automatic account-switching decisions for the same route. The existing proxy remains responsible for its account pool and refresh writer. Cylo holds only the downstream credentials needed to access the route; their real values must not enter the repository or command-line arguments.

Loopback port 8317 on the remote host cannot be addressed as `127.0.0.1:8317` from another machine. Prefer the HTTPS endpoint already used by the user. If Cylo eventually runs on the same host as the proxy, use a host-reachable loopback address or a controlled container network. Confirm the endpoint, TLS, and access controls for the deployment location; this review did not change the existing network.

The source supports proxying login, and the user reports using it successfully; this review did not retest that behavior. The global network proxy for a login process, per-account proxying at runtime, and the browser's own egress are distinct configuration layers (see report 14). Reuse the existing successful path; the absence of a dedicated per-account login option in the reviewed source is not a reason to require a new login-proxy feature.

## Acceptance boundaries for later work

The current evidence supports choosing the existing deployment as the architecture direction. It does not establish that Cylo integration is complete. With authorization for implementation, first pin the running version and configuration baseline, then verify AO TUI/app-server tool round-trips, complete history, compact/resume, switching after a 429 before generation, and recovery after output has already been emitted. Test HTTP Responses/SSE and WebSocket separately. The stream interruption, `previous_response_id`, and cross-account state boundaries described in report 14 still apply.

This review did not perform those product acceptance checks and did not establish concurrency limits, the number of usable accounts, quota behavior, subscription switching, actual network egress, or restart recovery. The Space voice gateway and its credential lifecycle remain independent; the existence of a text proxy does not justify replacing Space or sharing token files.
