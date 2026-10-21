# Voice: Official API Documentation Excerpts (Comparison/Historical Evidence)

This appendix preserves previously captured official facts for comparison and historical evidence only. It is not evidence of Space3044's private web-protocol capabilities and no longer supports selecting an STT+TTS main path. The current choice is a real-time web voice gateway and account pool; see [03](../reports/03-voice.md).

Retrieved: 2026-10-21. The excerpts below are from pages that actually returned HTTP 200; line breaks come from HTML text extraction. Model availability, pricing, and account access were not verified through API calls.

## live-transcription

https://developers.openai.com/api/docs/guides/realtime-transcription

HTML SHA-256: `0e45f2da648ceae0c7c4129c6e8186d423b40d9370c8bd9d3229b1a5f4ecfa60`

```text
Transcribe live audio in a Realtime session.
Copy Page
Use realtime transcription when your application needs text from a microphone, call, or other live audio stream without a spoken assistant response. The recommended model returns transcript deltas as speech arrives and a final transcript when your application commits each audio turn.
Start with
gpt-live-transcribe
. Use
file transcription
if your audio is already recorded, or see the
Transcription overview
to compare the workflows.
Create a transcription session
Create a session with
type: "transcription"
and select
gpt-live-transcribe
. Connect with
WebSocket
for server-side audio pipelines or
WebRTC
for browser audio.
1
```

```text
}
}
}
}
This example uses 24 kHz PCM audio. In
gpt-live-transcribe
transcription sessions, omit
audio.input.turn_detection
or set it to
null
. The model doesn’t support
server_vad
or
semantic_vad
. For the full session configuration, see the
Realtime sessions reference
.
Stream audio
Send audio chunks with
input_audio_buffer.append
:
```

```text
.
stringify
({
type:
"input_audio_buffer.commit"
,
})
);
Use client-side VAD to detect the end of speech, then send
input_audio_buffer.commit
.
Handle transcript events
Listen for incremental transcript deltas and completion events:
```

```text
:
0
,
"transcript"
:
"Hello, how are you?"
}
Ordering between completion events from different speech turns isn’t guaranteed. Use
item_id
to match transcription events to committed input items.
Add transcription context
Add context when the audio contains specialized vocabulary or more than one expected language. Send another
session.update
```

```text
when accuracy matters more than immediate display;
xhigh
when your workflow can tolerate the most delay for more context.
The exact delay in milliseconds can vary by model configuration, so benchmark with representative audio instead of assuming a fixed timing per level.
Don’t choose a setting from synthetic audio alone. Test with representative microphones, telephony audio, accents, background noise, code-switching, domain vocabulary, and long sessions.
Handle confidence, timestamps, and speaker labels
gpt-live-transcribe
doesn’t return word-level timestamps, speaker labels, or transcription confidence scores. If your application requires timestamps or speaker labels, use a compatible
file transcription
model or add an application-level fallback.
Production checklist
Pick a target latency and accuracy threshold before tuning.
Test against real production audio, not only clean samples.
```

## vad

https://developers.openai.com/api/docs/guides/realtime-vad

HTML SHA-256: `2341b6f3237d918d0ce2366ee209c228ba30efdcf3fb8bede97c4880cb4d82cd`

```text
Voice activity detection (VAD) is a feature available in the Realtime API allowing to automatically detect when the user has started or stopped speaking.
It is enabled by default in
speech-to-speech
Realtime sessions, but is optional and can be turned off.
In
transcription
Realtime sessions, turn detection support depends on the transcription model. Models that support VAD default to
server_vad
, while
gpt-live-transcribe
and
```

```text
session.audio.input.turn_detection
field in transcription sessions. For
gpt-live-transcribe
and
gpt-realtime-whisper
, omit turn detection or set it to
null
.
The
create_response
and
interrupt_response
fields are only used in speech-to-speech conversations. In transcription sessions, VAD only controls how audio is chunked.
Semantic VAD
```

## tts

https://developers.openai.com/api/docs/guides/text-to-speech

HTML SHA-256: `2c3f7b0edd0a351963ab55b71518c79e87c0d07807d58788344f8a85f590133d`

```text
Learn how to turn text into lifelike spoken audio.
Copy Page
The
tts-1
,
tts-1-hd
, and
gpt-4o-mini-tts
models are deprecated and will
shut down on
January 6, 2027
. Migrate to
gpt-realtime-2.1-mini
using the
Realtime API
. This is a change of API workflow:
gpt-realtime-2.1-mini
isn’t a supported model for
/v1/audio/speech
.
Existing integrations can use the Speech API workflows shown below until
shutdown. See the
deprecation
notice
.
The Audio API provides a
speech
endpoint based on our
GPT-4o mini TTS (text-to-speech) model
```

## deprecations

https://developers.openai.com/api/docs/deprecations

HTML SHA-256: `69a03aee80b2b1ab255b5d4cd7d13e09100f7160f6191201a9658424cdb1c372`

```text
2026-10-01: Text-to-speech models
The following text-to-speech models are deprecated and will be removed from the API on January 6, 2027, with at least three months’ notice. Migrate to
gpt-realtime-2.1-mini
before the shutdown date. See the
Realtime API guide
to plan your migration.
Shutdown date
Model / system
Recommended replacement
Jan 6, 2027
tts-1
gpt-realtime-2.1-mini
Jan 6, 2027
tts-1-hd
gpt-realtime-2.1-mini
Jan 6, 2027
gpt-4o-mini-tts-2025-03-20
gpt-realtime-2.1-mini
Jan 6, 2027
gpt-4o-mini-tts-2025-12-15
gpt-realtime-2.1-mini
2026-08-26: Transcription models
On August 26, 2026, we notified developers using
whisper-1
,
gpt-4o-transcribe
,
gpt-4o-mini-transcribe
, and
gpt-4o-transcribe-diarize
of their deprecation and removal from the API on February 26, 2027.
For information about the recommended replacements, see the
transcription guide
.
Shutdown date
Model / system
Recommended replacement
Feb 26, 2027
whisper-1
gpt-live-transcribe
or
gpt-transcribe
Feb 26, 2027
gpt-4o-transcribe
gpt-live-transcribe
or
gpt-transcribe
Feb 26, 2027
gpt-4o-mini-transcribe
gpt-live-transcribe
or
gpt-transcribe
Feb 26, 2027
gpt-4o-transcribe-diarize
gpt-live-transcribe
or
gpt-transcribe
```

## server

https://developers.openai.com/api/docs/guides/realtime-server-controls

HTML SHA-256: `d7fe797add3067c3e405923b77518b00e7a447b808a4c8955b269408dee1a4f3`

```text
Attach your application server to an existing GPT-Live WebRTC or SIP session when the server needs to receive conversation events, execute private tools, or update the conversation. This second connection is called a
sideband WebSocket
. Both connections share one session while WebRTC or SIP carries the primary audio.
The sideband carries events and commands. Your application supplies the tool execution, authorization checks, and business rules. Keep API keys and tool credentials on your server.
Decide whether you need a sideband
For browser applications, use the
WebRTC data channel
for captions and local UI updates. Use a sideband when transcript processing runs on your server, such as guardrail checks, sentiment analysis, or speculative tool calls. Your server can receive events and steer the same session directly while browser audio stays on WebRTC. See
React to transcript fragments
for examples.
If your backend streams audio over the primary
WebSocket connection
, use that connection to receive events and send commands.
Responses delegation
also works without a sideband. The browser can forward function-call events from its data channel to an authenticated backend for execution. OpenAI-hosted tools run through the delegated backend without an application tool executor.
```

```text
For disclosures that request specific spoken wording, also use instructions. See
Deliver a disclosure
for an example and playback considerations.
Control playback when needed
If your application needs to block model audio, control playback at the client or media relay. Temporarily mute or drop the output, discard locally queued audio, and send the corrective instruction. Resume playback according to your application’s recovery policy after clearing stale audio; an instruction acknowledgment is not a signal to resume. A sideband alone does not control playback, and corrective instructions cannot retract audio the caller has already heard.
session.input_audio.mute
controls the caller’s microphone input. It does not mute model output or cancel delegated work.
Check speech before playback
For most applications,
monitor user and assistant transcripts
while the conversation continues. When a guardrail triggers, your application can block affected actions or send corrective instructions.
If your application needs to check assistant speech before playback, buffer the audio in your player or media bridge before sending it to the caller. Keep receiving audio and transcript events while the check runs, and read transcripts independently of playback.
Collect audio and its transcript.
Implement output voice activity detection (VAD) in your application, or use a noise gate provided by your media framework, to identify candidate speech segments. GPT-Live does not provide an output VAD or noise gate for this workflow. Wait for the transcript needed to check each segment.
Release approved audio.
When a segment passes the check, enqueue its original buffered audio for playback. If the check fails, the transcript is missing, or the check times out, discard the segment and use an application-defined safe fallback.
Handle interruptions.
Associate the audio, transcript, check result, and playback state with an application-generated ID. When an interruption cancels that speech, clear its buffered and queued audio and ignore any later approval for it.
A pause can mark a candidate segment while the model is still composing an answer. If your policy requires checking a complete answer, define how your application establishes completion; voice activity detection alone cannot establish that the whole answer is finished.
Buffering adds latency. Discarded speech remains in the model’s conversation context, so test how the conversation continues after withheld audio or a fallback.
```

## conversations

https://developers.openai.com/api/docs/guides/realtime-conversations

HTML SHA-256: `9206ba0c522161e9c68bfcb3d67ed6f02de07f534885ab9167930ccf2f033f57`

```text
Interruption and Truncation
In many voice applications the user can interrupt the model while it’s speaking. Realtime API handles interruptions when VAD is enabled, in that it detects user speech, cancels the ongoing response, and starts a new one. However in this scenario you will want the model to know where it was interrupted, so it can continue the conversation naturally (for example if the user says “what was that last thing?”). We call this
truncating
the model’s last response, that is, removing the unplayed portion of the model’s last response from the conversation.
In WebRTC and SIP connections the server manages a buffer of output audio, and thus knows how much audio has been played at a given moment. The server will automatically truncate unplayed audio when there’s a user interruption.
With a WebSocket connection the client manages audio playback, and thus must stop playback and handle truncation. Here’s how this procedure works:
The client monitors for new
input_audio_buffer.speech_started
events from the server, which indicate the user has started speaking. The server will automatically cancel any in-progress model response and a
response.cancelled
event will be emitted.
When the client detects this event, it should immediately stop playback of any audio currently being played from the model. It should note how much of the last audio response was played before the interruption.
The client should send a
conversation.item.truncate
event to remove the unplayed portion of the model’s last response from the conversation.
Here’s an example:
```

## transcription

https://developers.openai.com/api/docs/guides/speech-to-text

HTML SHA-256: `2b79cfa5aa828124e3f93f14d14ab02258c65f6ec8450595c24eb753ef978ce4`

## mdn-mic

https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia

HTML SHA-256: `dc68e6fb9649d29dff250adca448b70ec91021f2e2161b05358d5f92ebb8c5cf`

```text
.
This can also happen if you try to call
getUserMedia()
in an insecure context, since
navigator.mediaDevices
is
undefined
in an insecure context.
Privacy and security
As an API that may involve significant privacy concerns,
getUserMedia()
's specification lays out a wide array of privacy and security requirements that browsers are obligated to meet.
getUserMedia()
is a powerful feature that can only be used in
secure contexts
; in insecure contexts,
navigator.mediaDevices
is
undefined
, preventing access to
getUserMedia()
.
A secure context is, in short, a page loaded using HTTPS or the
file:///
URL scheme, or a page loaded from
localhost
.
In addition, user permission is always required to access the user's audio and video inputs.
```

## mdn-page

https://developer.mozilla.org/en-US/docs/Web/API/Page_Visibility_API

HTML SHA-256: `fcfb03cafd4c0ac66c5cf4ac469242c27c89521fd9c7d9e52dad487af463b584`

```text
Policies in place to aid background page performance
Separately from the Page Visibility API, user agents typically have a number of policies in place to mitigate the performance impact of background or hidden tabs. These may include:
Most browsers stop sending
requestAnimationFrame()
callbacks to background tabs or hidden
<iframe>
s in order to improve performance and battery life.
Timers such as
setTimeout()
are throttled in background/inactive tabs to help improve performance. See
Reasons for longer delays than specified
for more details.
```

## mdn-speech

https://developer.mozilla.org/en-US/docs/Web/API/SpeechRecognition

HTML SHA-256: `dda55c2686c9606ea8c936d2b8ea6288112dbad50669c49e9b84d3047faf015f`

```text
Limited availability
This feature is not Baseline because it does not work in some of the most widely-used browsers.
Want more browser support for this feature?
Tell us why.
See full compatibility
Learn more
The
SpeechRecognition
interface of the
Web Speech API
is the controller interface for the recognition service; this also handles the
SpeechRecognitionEvent
sent from the recognition service.
Note:
On some browsers, like Chrome, using Speech Recognition on a web page involves a server-based recognition engine. Your audio is sent to a web service for recognition processing, so it won't work offline.
EventTarget
SpeechRecognition
```
