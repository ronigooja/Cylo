# Real-Time Voice and Account-Pool Review Evidence

Date: 2026-10-21; research scope. This supports [03](../../../research/03-voice.md). No real accounts, calls, devices, or billable API requests were used.

## Source and protocol

Both `git -C /tmp/cylo-research-voice rev-parse HEAD` and `git -C /tmp/cylo-research-voice ls-remote origin HEAD` returned:

```text
6338ac478070aaa34bfe7a3c0469caf9e9174c02
```

Precise locations below are all at this SHA and do not depend on a future `main`:

- `internal/voice/service.go:628 buildSessionJSON`: `client_tools: []any{}`; `conversation_mode.kind=primary_assistant`; `history_and_training_disabled=false`; no Cylo tools/instructions bridge. Comments say upstream may ignore conversation/parent fields and still create a new session.
- `service.go:740 ProbeAccountToken`: settings/user 200 JSON → alive; 401 → disabled; other outcomes → unknown. The implementation branches on `status == 401`, although the comment emphasizes JSON and the code does not check JSON first. Do not claim as a guarantee that “all HTML 401 responses will never disable an account.”
- `service.go:943 CreateSession`: live binding → account ID → durable account → LRU; a failed sticky account is not silently replaced. The 401 branch at `1085` disables the account; other non-2xx/non-SDP results are uniformly mapped to 502 after `1113`, with no further rotation and no quota/reset fields retained.
- `internal/accounts/pool.go`: `Pick` chooses by LRU rather than reserving a slot, `PickByID` retains an account, and `MarkInvalid` disables one. There are no remaining/reset/cooldown fields.
- `internal/api/downstream.go:25`: downstream Bearer key identifies an owner; downstream state keeps only `voice_session_id` and returns no account/token. `context` stores IDs and does not inject model conversation content.
- `static/voice.html:4081 buildBaseUserMessage` constructs `author.role='user'`; `3822 sendRelayMessageTo` sends a two-layer `relay_message` to the upstream DataChannel. `4285 sendTextMessage` and the preview greeting near `3925` use this candidate upstream-input path; they are not messages sent between clients. The lower-level `sendDcEventTo` wraps it again as `{type:"data_message", data:JSON.stringify(eventObj)}`. Any reuse must preserve this outer wrapper rather than sending an official Realtime event directly.
- `static/voice.html:3849 applyCaptionDelta` reads `message.author.role` or `audio_transcription.direction(in/out)`, full text, and appended deltas. The caption receiver is near `4193`; substring matching against transcripts near `4224` is compatibility logic only. There is no reliable final-state check, and `rememberDcText` also stores partials; captions cannot directly trigger business execution.
- `static/voice.html:3773 interruptAssistant` sends only the private `action_request/stop_speaking`; it does not mute locally. Sending the stop event proves neither that audio stopped nor that the next autonomous reply is prohibited.
- `internal/tokenutil/jwt.go` decodes `exp` with signature verification explicitly skipped, for display only; this is not a quota reset.

Accurate observations in the prior report remain: MIT license, direct media, no automatic refresh, CSP blocking iframes, browser AEC request, RMS/RAF interruption, and releasing a binding does not terminate an established WebRTC session. The earlier choices “do not use the account/upstream” and “make STT+TTS the main path” were corrected by this review and are no longer valid.

## Offline checks actually run

```text
$ python3 docs/archive/research/evidence/voice-pool-poc.py /tmp/cylo-research-voice
PASS source assertions: empty client_tools, 401/502 branches, LRU/sticky, relay/stop construction
PASS mock: sticky reacquire; two slots; concurrent third selection rejected
PASS mock: exhausted account excluded; replacement gets new generation and clears upstream context
PASS mock: stale epoch rejected; repeated committed turn produces one command
PASS mock: local cooldown retry eligibility; unconfirmed media closure quarantined; disabled excluded
NOT TESTED: actual Go code execution, transactions/concurrency races, upstream quota/reset, WebRTC, final boundary, relay acceptance, tools, sideband, speech fidelity

$ node docs/archive/research/evidence/voice-static-check.cjs /tmp/cylo-research-voice
PASS: 2 inline scripts pass node --check
PASS: active stop_speaking, duplicate suppression, muted mic guard, text interruption
NOT TESTED: DOM, WebRTC, audio, upstream acknowledgement, Chinese recognition, mobile devices
```

The Python account scheduler is a **single-process, serial, in-memory mock**. It proves only that the proposed invariants hold for the supplied simulated transitions. It does not prove concurrency-race/database-transaction implementation, execution of real pool logic, or upstream quota behavior. The script's 60 seconds is an example for local retry policy, not an upstream reset. The JS check extracts real functions and runs them with stubs; it has no DOM/media. There was no Go toolchain, so no `go test` or build was run.

## Official comparison material: fetched again

The environment had no documentation search tool. A request to `https://developers.openai.com/search?q=Realtime%20function%20calling%20sideband` returned 404; the official pages below were then fetched directly. This section proves official API capabilities only and does not establish anything about the private web protocol.

1. [Realtime server controls](https://developers.openai.com/api/docs/guides/realtime-server-controls) redirects to `voice-server-controls?voice-api=realtime`; SHA256 of the original HTML retrieved over HTTP this round: `d7fe797add3067c3e405923b77518b00e7a447b808a4c8955b269408dee1a4f3`.

   Original text in the Realtime branch:

   > using that call ID with the URL wss://api.openai.com/v1/realtime?call_id=rtc_xxxxx

   > In this way, you are able to add tools, monitor sessions, and carry out business logic on the server instead of needing to configure those actions on the client.

   The example uses `OPENAI_API_KEY` and `session.update`. The same page's Live `/v1/live/sessions/{session_id}/attach` and `session.instructions.append` are a separate branch; do not mix them with Realtime or ChatGPT Web UUIDs.

2. [Realtime conversations](https://developers.openai.com/api/docs/guides/realtime-conversations), HTTP 200, SHA256: `9206ba0c522161e9c68bfcb3d67ed6f02de07f534885ab9167930ccf2f033f57`.

   Original text:

   > Create a new text conversation item using the conversation.item.create client event.

   > The conversation item type is function_call_output

   > Once we have added the conversation item containing our function call results, we again emit the response.create event from the client.

These support tool results and session instructions in the official protocol, but the upstream web source does not implement them. The existing [official excerpts](voice-official-excerpts.md) preserve facts from previously fetched transcription/TTS/deprecation material. They are retained only as comparison/historical evidence, not to choose this round's main path or claim that web voice can use standalone STT/TTS.

## Three bridge gates that require real validation

1. Reproducible final/completion signal for private user captions, including role, revisions, reordering, and deduplication. A silence timer must not masquerade as upstream finality.
2. A real Cylo receipt must enter the **same** real-time session through the user-role relay and be spoken back correctly. Check each number, negation, and task state; testing only `send()` or visible captions is insufficient.
3. The upstream can answer on its own after receiving microphone input; there is currently no evidence of a no-reply control or authorization-identity injection. Test whether premature “completed” and duplicate answers can be prevented. Prompts and `stop_speaking` do not guarantee this by themselves.

If any gate fails, the capability can only be described as “candidate native real-time voice conversation + independent Cylo task state”; **the business voice loop has not passed acceptance**. Continue research and changes for the selected upstream/account pool rather than claiming completion through independent TTS.
