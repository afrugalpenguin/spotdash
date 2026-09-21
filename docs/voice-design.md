# Local voice commands: design

Design for issue #73. Nothing here is built, and no source, dependency, manifest or config change comes with this document.

Voice is local: the recogniser runs on the PC that already runs the agent, and no audio or text leaves the machine. The command list is closed: "next face", "previous face", "show calendar", "show spotify", "show clock", "pause", "stop", "play", "next track" and "previous track".

## Microphone location

The decision is the PC's microphone, captured by the agent. The Spot's microphone would need native capture in the shell. The panel is served over plain http (`agent/internal/app/app.go`) and MDN says `getUserMedia()` exists only in secure contexts. The shell has no capture: `PanelActivity.kt` does not override `onPermissionRequest`, the manifest lacks `RECORD_AUDIO`, and the WebSocket is one way (`agent/internal/server/ws.go` discards incoming frames). `docs/device.md` lists the microphone as experimental, and the owner reports the mute switch does not work, so it would have no physical off switch. A Windows privacy setting can block desktop apps from the PC's microphone, and how a recogniser reports that is not stated.

## Recogniser

The decision is Windows built-in recognition (`SpeechRecognitionEngine`) with a constrained phrase list, run in a helper process. The agent starts `powershell.exe` and reads one line per recognised command from its output. Windows PowerShell 5.1 already has `System.Speech`, so the Go module gains no dependency and the `CGO_ENABLED: "0"` release build (`.github/workflows/release.yml`) is untouched. The costs are a resident `powershell.exe`, a need for PowerShell 5.1 and an English recogniser, and a possible antivirus objection (unchecked). The documentation does not say that no audio leaves the machine, so the test checks that.

The fallback is Vosk (Apache-2.0, offline), which takes a phrase list and "might return [unk] if user said something different". It does not capture audio, and whether its Go binding needs cgo is not stated. Local Whisper (MIT) is heavier, and its Go bindings need cgo. No documentation gives idle CPU, latency or false-trigger figures, so those are measured.

## Trigger

The decision is a global push-to-talk hotkey for the first build. `RegisterHotKey` would be called through `golang.org/x/sys/windows` from a goroutine with a message loop, which the repo has none of today. The recogniser is armed for six seconds after the press, or until one command is recognised, because the documentation describes press notifications only. If registration fails, voice must switch off and never fall back to always listening.

A spoken prefix such as "spot, next face" costs CPU continuously, and on a call the owner's own words and speaker audio reach the recogniser. It could become an opt-in setting only if the soak day below shows no false triggers that matter.

## Command mapping

"pause" and "stop" map to `pause` (the reference pages list no stop endpoint), "play" to `resume`, and "next track" and "previous track" to `next` and `previous`. `playbackController` has exactly these four methods (`agent/internal/sources/spotify/apisource.go`), and voice keeps that surface. The switch that runs them sits inside the HTTP handler `handleControl`, which exists only in `api` mode. Its body moves into `Control(ctx, action) error` behind an optional interface like `RepollRegistrar` (`agent/internal/sources/registry.go`), so the agent does not call itself over HTTP.

No new OAuth scope is needed, since `user-modify-playback-state` has been requested since `5cfd4a4` (#35). Only an older connection can lack it, and the agent cannot tell which scopes its stored token has. To check, send one `pause` while something plays. A 403 without `PREMIUM_REQUIRED` points at scope, but the code returns a generic 502, so the build should add a hint. To re-consent, open `http://127.0.0.1:8765/spotify/connect?token=<token from config.json>`. Whether Spotify shows the consent screen again is not stated.

Faces change today only in the panel's JavaScript (`agent/web/app.js`). Following `handleCalendarUrgency`, the voice source publishes a reading through the existing store and WebSocket, and `app.js` handles it. The server replays each source's latest reading on every reconnect (`ws.go`), so the reading carries a sequence number and a timestamp, and the panel applies it only if it is new and a few seconds old at most. "No active device" already becomes a 409 from behaviour recorded in the code, and the Spotify pages do not document it, so the build keeps a test for it. The panel cannot show it today (`agent/web/faces/spotify.js` hides the transport buttons when nothing is playing), so `app.js` shows a fixed message, never a transcript.

## Fit with the source model

Voice is event-driven and sources are polled. The decision is a source named `voice` plus one optional event-runner interface in the style of `RepollRegistrar`: the app calls `Run(ctx, env)`, and `env` offers a publish function and the playback controller. `Poll` stays cheap and returns listening and muted state. A source gets `/health`, the status face, config and reload validation without new code.

A reload tears the microphone down, so mute state belongs to `App`. The runner recovers panics in `Poll` but not in an event goroutine, so the voice goroutine must recover its own. `/health` is unauthenticated and must never carry a phrase. The config is a `sources.voice` block, off by default.

## Security and privacy

The agent would open a microphone for the first time. Voice is off by default and enabled only by editing `config.json`. A tray icon shows while the microphone is open, and a tray mute stops the recogniser and survives a reload. No audio or transcript is written to disk, and recognition makes no network call. Command names are logged at debug level only, and the log keeps 3 backups for 28 days (`agent/internal/logging/logging.go`), so they persist that long.

Voice fails closed: if the recogniser, helper, microphone or hotkey fails, the microphone is closed and the source reports `degraded`. There is no new HTTP route. Voice is not authentication, so anyone in earshot can issue a command, and the closed list limits that to pausing, skipping and changing faces. The security notes in `docs/architecture.md` say "No command endpoints, so a leaked token only reads", which is already wrong since `POST /spotify/control` changes playback.

## Accuracy test before building

A throwaway test on the owner's PC with the real microphone. Part 1 is 100 utterances in shuffled order, ten per phrase. Part 2 is three minutes reading a paragraph that uses "next", "stop", "play" and "pause" in ordinary sentences, then two minutes of speaker audio with the owner silent. Proposed pass marks, which the owner can change:

- Correct command: at least 95 of 100. Wrong action: at most 1 of 100.
- False triggers in Part 2: zero, including the speaker audio.
- Latency: median at most 1.0 s, worst at most 2.0 s.
- Idle CPU while armed and quiet: at most 2 percent, working set at most 150 MB.
- Privacy, Windows recognition run with the network disabled: no changed result, no outbound connection, no audio written.

Zero false triggers in five minutes still leaves an upper bound of about three per five minutes, so the real measure is one working day with `dry_run` on and `log_level: "debug"`. If Windows recognition passes, build it. If only Vosk passes, build Vosk.

## Open questions for the owner

Each has a recommendation.

1. The PC's microphone? Yes, and name the microphone to test.
2. Hotkey first, prefix later only if it passes the soak? Yes. Avoid conferencing shortcuts and F12.
3. A helper process running Windows PowerShell 5.1? Yes. It keeps `CGO_ENABLED=0`.
4. Are the pass marks acceptable, with a soak day in `dry_run` first? Yes to both.
5. Re-consent to Spotify now? Yes, one visit to `/spotify/connect` and a test `pause`.
6. Rewrite issue #73, which asks for a hosted service? Yes.
7. Mute kept across a config reload and reset on restart? Yes, it fails toward a closed microphone.

## What is unverified

Statements about the agent, panel and shell were checked against the code in this repository. Nothing was tried on the Spot, so its microphone and audio capture are untested. No recogniser has run on the PC, so accuracy, latency, idle CPU and whether the Windows recogniser sends audio anywhere are unmeasured. The XDA thread returned HTTP 403, so the mute switch statement comes from the owner's brief, and the Android pages for `RECORD_AUDIO` and `PermissionRequest` returned no content. The measured `MemTotal` on the device is 1958192 kB, about 2 GB.

## Sources

Spotify
- [Pause a user's playback](https://developer.spotify.com/documentation/web-api/reference/pause-a-users-playback)
- [Start/resume a user's playback](https://developer.spotify.com/documentation/web-api/reference/start-a-users-playback)
- [Scopes](https://developer.spotify.com/documentation/web-api/concepts/scopes)
- [Refreshing tokens](https://developer.spotify.com/documentation/web-api/tutorials/refreshing-tokens)
- [API calls: status codes and error object](https://developer.spotify.com/documentation/web-api/concepts/api-calls)

Windows
- [SpeechRecognitionEngine class](https://learn.microsoft.com/en-us/dotnet/api/system.speech.recognition.speechrecognitionengine)
- [RecognizedPhrase.Confidence](https://learn.microsoft.com/en-us/dotnet/api/system.speech.recognition.recognizedphrase.confidence)
- [Speech API overview (SAPI 5.4)](https://learn.microsoft.com/en-us/previous-versions/windows/desktop/ee125077(v=vs.85))
- [SpeechRecognitionListConstraint](https://learn.microsoft.com/en-us/uwp/api/windows.media.speechrecognition.speechrecognitionlistconstraint)
- [Differences between Windows PowerShell 5.1 and PowerShell 7.x](https://learn.microsoft.com/en-us/powershell/scripting/whats-new/differences-from-windows-powershell)
- [RegisterHotKey](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerhotkey)
- [Windows camera, microphone and privacy](https://support.microsoft.com/en-us/windows/windows-camera-microphone-and-privacy-a83257bc-e990-d54a-d212-b5e41beba857)

Vosk and whisper.cpp
- [Vosk](https://alphacephei.com/vosk/), [install](https://alphacephei.com/vosk/install), [models](https://alphacephei.com/vosk/models)
- [vosk-api repository](https://github.com/alphacep/vosk-api) and its [C header](https://raw.githubusercontent.com/alphacep/vosk-api/master/src/vosk_api.h)
- [whisper.cpp](https://github.com/ggml-org/whisper.cpp) and its [Go bindings](https://github.com/ggml-org/whisper.cpp/tree/master/bindings/go)

Web and Android
- [MDN: getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia)
- [Android source, WebChromeClient.onPermissionRequest Javadoc (android11-release)](https://android.googlesource.com/platform/frameworks/base/+/refs/heads/android11-release/core/java/android/webkit/WebChromeClient.java)
- [Android permissions overview](https://developer.android.com/guide/topics/permissions/overview)

Device
- [XDA: LineageOS 18.1 for the Echo Spot 2017 (rook)](https://xdaforums.com/t/rom-unofficial-11-rook-lineageos-18-1-for-the-amazon-echo-spot-2017.4762459/). Could not be fetched; the mute switch statement is from the owner.
