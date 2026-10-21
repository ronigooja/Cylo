# CLIProxyAPI and AO Account-Pool Integration Review

Reviewed: 2026-10-21. Conclusion: **CLIProxyAPI is already present in the user's infrastructure, and local Codex is configured with a custom Responses provider; Cylo should reuse this deployment rather than introduce a second proxy.** The user reports that both login and use already go through the proxy. This round performed a read-only review of deployment/configuration and did not retest login or model calls. See the [existing deployment review](15-existing-cliproxy-deployment.md) for deployment evidence and reuse plan. AO already manages multiple Codex accounts. If the goal includes unattended switching after subscription quota exhaustion, the behavior of the existing proxy together with AO/Codex still requires acceptance. AO manages sessions/tasks; CLIProxyAPI selects upstream accounts for one model-request route. The proxy is neither a required AO dependency nor a replacement for the selected Space voice gateway. No AO fork is needed for the first round: reuse the custom Responses provider and accept tool use, continuation, and failure recovery.

## Pinned baseline and evidence scope

Reused the existing clean clone `/root/CLIProxyAPI`. Did not access existing user auth files, log in, refresh a real token, or call a model. Did not pull; conclusions apply only to the snapshot already present, not the latest release.

| Item | Checked value |
|---|---|
| Repository | https://github.com/router-for-me/CLIProxyAPI.git |
| SHA | `091ff27f31285ec4fd1dd5823b44f7f51fb8f48b` |
| Commit time | `2026-10-12T06:38:29+08:00` |
| Commit subject | `feat(antigravity): persist model capability hints in auth metadata` |
| Module / Go | `github.com/router-for-me/CLIProxyAPI/v8` / `go 1.26.0`; see root [go.mod](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/go.mod#L1) |
| License | Root [LICENSE](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/LICENSE#L1) is MIT; this was checked directly, not inferred from README or dependency licenses. |
| AO comparison baseline | `OrchestratorInc/AO`, `7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`, local `/tmp/cylo-alt-ao` |
| Space comparison baseline | `Space3044/chatgpt-web-voice`, `6338ac478070aaa34bfe7a3c0469caf9e9174c02` |

All CLIProxy source links below pin the SHA above. The reviewer read the OpenAI Docs skill, searched in its specified order, and fetched the [official Codex configuration reference](https://developers.openai.com/codex/config-reference/). Official material is used only to establish Codex configuration semantics; it does not prove a third-party proxy is authorized for subscription access or has model access/quota.

## AO already supports multiple accounts; why might a proxy still help?

AO's `backend/internal/adapters/agentaccount/codex.go`, `backend/internal/service/agentaccounts/service.go`, and `backend/internal/session_manager/agent_account.go` already support account import, default account, per-session binding, and explicit switching. Each account uses a separate `CODEX_HOME/auth.json`; configuration and history, among other data, are shared. Chat and TUI take different code paths. Do not treat global `/codex/accounts` switching as an account switch for any agent.

In fixed AO, the `usage_limit.go` path lets a user enable recovery for a particular failed Chat turn, waits for reset, then resumes with the same account. The review did not establish automatic scanning of another account on 429. **“Manage multiple accounts,” “manually switch the same thread,” and “request-level automatic cooldown/failover” are three different capabilities.** CLIProxy mainly adds the third, plus a centralized pool for multiple clients and protocol conversion. If account isolation and manual selection are enough, AO native features suffice. If unattended execution should continue after subscription exhaustion, reuse the existing proxy to test automatic switching; a turn that has already started emitting still requires AO/Codex error recovery.

## CLIProxy OAuth account pool: implementation and limits

| Capability | Source review | Meaning for Cylo |
|---|---|---|
| Codex/ChatGPT OAuth login and refresh | [sdk/auth/codex.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/auth/codex.go#L40) implements PKCE/state/browser callback and device flow; [token.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/auth/codex/token.go#L19) stores access/refresh/ID token, `account_id`, and `plan_type`; [executor_auth.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_executor_auth.go#L34) refreshes | This is Codex OAuth upstream support, not conversion of a ChatGPT subscription into an official API key. Real acceptance is still needed to establish which model a given account can use. |
| Multiple-credential selection | [selector.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/selector.go#L614) implements round-robin, weighted round-robin, and fill-first after filtering by availability/priority. | More than putting one token behind a base URL; priorities and model-availability constraints apply. It does not combine quotas across accounts. |
| Session affinity | [selector.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/selector.go#L1050) plus session resolver/cache; [example config](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/config.example.yaml#L143) defaults to affinity=false, TTL=1h, with subagents inheriting by default when enabled. | Enable affinity in the PoC. Headers/session/`prompt_cache_key`, etc. determine binding. A failed binding can fall back to another account, but this does not share upstream conversation/cache across accounts. |
| 429/quota cooldown | [executor_terminal.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_executor_terminal.go#L403) distinguishes `usage_limit_reached` and parses `resets_at` / `resets_in_seconds`; [conductor_cooldown.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_cooldown.go#L909) records unavailable/recovery time at model or credential scope. | Quota may cool down the whole account by default; model-level cooling is explicit. Ordinary rate limits do not equal subscription exhaustion. Missing reset data uses fallback/backoff and cannot promise exact remaining quota. |
| Retry and switching | [conductor_execution.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_execution.go) and [conductor_stream.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_stream.go) filter candidates and record outcomes; configuration sets extra rounds, per-round credential limit, and maximum cooldown wait. | Defaults: 3 extra rounds; 0 credentials per round means try all available credentials; maximum wait 30 seconds. It does not wait indefinitely for an account to recover. Business deadline should cap total waiting. |
| Persisted cooldown | `routing.cooldown.save-cooldown-status` defaults to false and writes `.cds` when enabled. | In-memory cooldown is lost on restart by default. Decide explicitly whether to persist it so restarts do not hit an exhausted account again. |
| Concurrency | Selector/cache use locks and serve concurrent requests. [home_concurrency.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/home_concurrency.go#L23) has Home credential admission/lease/busy types; [configuration notes](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/config.example.yaml#L108) stress that Home is the policy authority. | Round-robin/thread safety does not mean concurrency=1 per account and does not prove a standalone, configurable hard per-account concurrency limit. Initially limit Worker concurrency in AO/Cylo and measure actual in-flight work per account; validate Home distributed policy separately for a multi-instance pool. |

## Responses, tools, and continuation: do not promise seamless switching

For Codex OAuth routes, the [HTTP SSE executor](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_executor_stream.go#L24) defaults to `https://chatgpt.com/backend-api/codex`. The [Responses translator](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/translator/codex/openai/responses/codex_openai-responses_request.go#L19) forces `store=false` and handles items such as `function_call`. Native Codex tool schemas, `parallel_tool_calls`, reasoning, `apply_patch`, compact, etc. have dedicated adaptations, but that does not prove equivalent tool semantics across every version/proxy protocol.

**The HTTP path explicitly removes `previous_response_id`** for both streaming and non-streaming requests. A client that sends only `previous_response_id` plus a delta and expects the server to retrieve the old response cannot be assumed compatible. Native Codex should send complete history/tool `call_id` pairs; test reasoning/encrypted items and history after compaction. Affinity helps caching and identity stability; it cannot restore missing history or create cross-account response storage.

[Downstream WebSocket request normalization](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/api/handlers/openai/openai_responses_websocket_requests.go#L94) has two paths: when deltas are supported it retains `previous_response_id`; otherwise it merges that socket's lastRequest/lastResponseOutput/input and removes the previous ID. Full transcript and compaction also have replace/dedup handling. This adapts state within one connection; do not assume a proxy restart or new socket retains all old state.

[WS duplex](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_websockets_duplex.go) and `upstream.codex.response-steering` are experimental. Configuration comments say one socket is pinned to one upstream account/model and accepted input is not replayed. WS routing/reconnection does not promise that the same upstream response can continue after switching accounts at an arbitrary point. Initially disable the Codex provider WebSocket in the PoC and validate HTTP Responses/SSE; enable WS separately afterward.

**A stream that has started is not transparently replayed.** The [existing upstream mock test](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_stream_quota_test.go#L21) explicitly covers quota after `response.created`: preserve payload+error, attempts=1; only the next request chooses a backup account. `stream-bootstrap-buffering`, off by default, can buffer early empty frames and switch on overload/rate limit before generation, subject to frame/byte budgets and whether output has been committed. Timeout checks also run as frames arrive; they are not an independent hard timer. Do not describe this as automatic continuation halfway through generation.

The PoC should cover: A returns 429 with reset and B takes over the same request before output; A has emitted output and then fails, with an explicit failure and B selected for the next request; tool call → local execution → tool output, with no duplicate execution after switching and matching `call_id`; full-history and previous-ID-only request forms; compact/restart/resume; WS disconnect, identity binding, and cancellation. **None of these tests ran this round.** Do not “fix” non-idempotent tools by replaying a whole turn.

## Integrate AO by configuration first; no fork needed

The path is `AO → native Codex CLI/app-server → CLIProxy /v1/responses → Codex OAuth pool`. AO still schedules and manages agent sessions; Codex manages its local thread, history, and tool execution. The proxy changes only the model upstream.

Current official Codex configuration supports `model_provider`, `model_providers.<id>.base_url/env_key/wire_api/requires_openai_auth/supports_websockets`; `wire_api` currently supports only `responses`. This is a minimal user configuration awaiting validation; it does not hardcode a model name and should use one the user can actually access:

```toml
# Write to the CODEX_HOME/config.toml actually used by AO/Codex
model_provider = "cylo_pool"

[model_providers.cylo_pool]
name = "Cylo local pool"
base_url = "http://127.0.0.1:8317/v1"
wire_api = "responses"
env_key = "CYLO_CODEX_PROXY_KEY"
requires_openai_auth = false
supports_websockets = false
```

`CYLO_CODEX_PROXY_KEY` is the **CLIProxy downstream access key**, not an upstream OAuth token. Pass it to Codex launched by AO through a controlled process environment; never put the real value in the repository, prompt, command-line arguments, or documentation. Official reference says provider-related fields cannot go in a project-local `.codex/config.toml`; use user config or that `CODEX_HOME` config. Official docs may be newer than the Codex version used by AO, so pin the CLI version and revalidate at deployment.

- **TUI:** Fixed AO source `backend/pkg/agentruntime/command.go:313` `buildCodexLaunch` and `:322` restore start native `codex`/`codex resume`. That process reads its own config; AO's agent type does not need to be replaced with an HTTP agent.
- **Chat/app-server:** `backend/internal/adapters/chatdriver/codexappserver/driver.go:516` connect passes runtime env; `:575` and `:724` start the same binary's `app-server`. That server's stdin/stdout RPC is AO's control protocol with Codex. **Do not point its address to CLIProxy.** Configuration changes the Responses requests app-server sends to the model.
- Each AO native account's `CODEX_HOME/config.toml` is a shared link; direct edits can affect those accounts. For the first test use a dedicated HOME/instance. Before launch, inspect config precedence, environment, and AO model/list and selection behavior. Do not delete AO state or user auth files to connect it.
- Keep one authority for account selection on each route. If the proxy owns the OAuth pool for a model route, AO must not run a second automatic account switch for that route. The account name displayed by AO may identify only local configuration, not the proxy's actual upstream account. Record redacted provider/route/auth-id for audit; never record tokens.

Evaluate a small adapter/UI change only if AO lacks a dedicated provider-config entry, secret injection, or a way to display proxy selection. A normal `base_url` configuration is not a reason to fork AO.

## Per-account network proxies: supported, but distinguish login from runtime

**Proxying login is implemented and the user reports using it; this round did not retest it.** The following distinguishes login-process global egress from per-account runtime egress. No dedicated per-account login option was found; that does not mean login cannot use a proxy.

A network proxy (HTTP/HTTPS/SOCKS egress) differs from `model_providers.*.base_url` (model endpoint). CLIProxy OAuth JSON can have a top-level `"proxy_url": "http://127.0.0.1:19081"`; different account files can set different values. This is an added field on an existing auth record, **not a standalone complete credential file**. API-key credential groups use the `proxy-url` config field.

[`Auth.ProxyURL`](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/types.go#L77) is a formal field; the [file synthesizer](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/watcher/synthesizer/file.go#L174) reads it from OAuth JSON. Runtime priority is **internal request proxy override > per-account `auth.ProxyURL` > global `requests.proxy-url` > underlying transport/environment default**; see [HTTP helper](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/helps/proxy_helpers.go#L127). The [WS dialer](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_websockets_connection.go#L184) uses the same order. `direct`/`none` explicitly bypasses global/environment proxy. Do not misrepresent the internal request override as a supported header any client can send.

Token refresh uses `NewCodexAuthWithProxyURL(cfg, auth.ProxyURL)`, so it uses the per-account egress. **Initial login is different:** `sdk/auth/codex.go` creates `NewCodexAuth(cfg)`; login options in `cmd/server/main.go:654` contain NoBrowser/CallbackPort, etc. No per-account login-proxy option was found this round. Initial code/device exchange uses the login process's global `requests.proxy-url`. To give a new account a fixed login egress, log in accounts one at a time using a temporary isolated config, then keep the same `proxy_url` on each corresponding auth record. The external browser's route to the authorization page is still controlled by browser settings; Go HTTP proxy cannot guarantee it.

An invalid proxy config is not a reliable fail-closed policy: if the HTTP helper cannot create a proxy, it logs a redacted warning/debug message and tries the context transport. Validate proxy addresses before deployment and test HTTP/SSE, WS, and refresh egress separately. Review AO's own per-account proxy support in its dedicated report; do not infer it from CLIProxy capability.

## Relationship to Space voice pool and security boundaries

Pinned Space source [service.go](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L27) uses `/realtime/wm?dcid=0` SDP/signaling, web access tokens, and account proxies, and manages bindings, voice failures, and sessions. The CLIProxy Codex route uses `/backend-api/codex/responses`, Codex OAuth token/refresh/account_id, and Responses text/tool streams. Although both may involve Bearer tokens, ChatGPT domains, or even the same login identity, **similar field names do not prove that token audience/scope, refresh lifecycle, or quota are interchangeable.**

Therefore do not share token files, refresh writers, availability/cooldown status, or active leases. Even for the same person, do not assume “voice cooldown = Codex exhausted.” Account aliases, proxy-egress configuration, and redacted status may be summarized in one control plane, but keep `voice-web` and `codex-oauth` as separate provider capabilities and credential owners. If the same account might be reused later, first verify authorization/scope and conflicts; do not implement reuse by copying tokens.

Although the CLIProxy snapshot also contains real-time-related code such as `internal/client/codex/live`, this review checked only the Codex Responses path. It did not prove those modules can replace Space's selected web real-time media protocol, mobile WebRTC, playback/interruption, Cylo tool bridge, or voice-account lease. **Keep the Space voice gateway.**

Security deployment boundaries:

1. Example `server.host=""` binds all interfaces. A local PoC should explicitly bind `127.0.0.1` and set a separate random `access.api-keys` value. This is a different permission from `management.secret-key`; management API is disabled by default when its key is empty. Keep remote management closed, and add network access control in production.
2. OAuth is plain-text JSON. `internal/auth/codex/token.go` uses a directory mode of 0700 but writes with `os.Create`; do not claim every save path forces 0600. New-file permissions depend on umask, and existing file permissions need checking. Use a dedicated user, umask 077, and encrypted backups. Do not collect auth directories in Git, ordinary logs, or tickets. This finding is from source review; actual user file permissions were not inspected.
3. Proxy URLs may contain credentials; treat tokens, HTTP bodies, tool arguments, and proxy passwords as sensitive. Example `request-log=false` does not prove every log/management export is free of leaks. Minimize logs and the management surface for the actual deployment. Do not let two services concurrently modify the same refresh-token file.
4. Pin source, Go toolchain, dependencies, configuration, and a rollback version. MIT is a software license, not authorization for subscription-service use or a quota promise. No official OpenAI commitment about this use was inferred from third-party source.

## Checks this round and next acceptance work

This round checked `git status/remote/log/show`, root license and go.mod, the source/available tests above, and fetched the official Codex page. `go` was not on PATH and was not found in common install locations, so **no Go test, mock, or real-subscription end-to-end check was run**. Upstream mock tests were read only to establish design boundaries.

In an isolated Go 1.26 environment, existing pure-mock targets could be run after individually checking that they have no real network/credential dependency: `TestCodexQuotaErrorCredentialScope`, `TestParseCodexRetryAfterQuotaLayouts`, `TestSessionAffinitySelector_FailoverWhenAuthUnavailable`, and `TestExecuteStreamQuotaFailurePreservesCooldownAndScope`. Passing them would still prove only local logic. A real PoC should then accept two controlled accounts, explicit per-account network proxies, switching on subscription 429, one complete tool round-trip each for native TUI and app-server, and compact/resume before considering unattended production use.
