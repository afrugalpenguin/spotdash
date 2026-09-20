# Local voice commands: design

Design for issue #73. Nothing here is built. No source, dependency, manifest or config change comes with this document.

The issue as written asked for a hosted voice service reached over the internet. This design drops that: voice is local, the recogniser runs on the PC, and no audio or text leaves the machine. The issue title and body should be rewritten to match (see the open questions).

## What this is

A short list of spoken commands that act on the panel and on Spotify, recognised on the PC that already runs the agent:

- change face: "next face", "previous face", "show calendar", "show spotify", "show clock"
- playback: "pause", "stop", "play", "next track", "previous track"

It is a closed list. There is no free-form speech, no spoken replies, no wake-word model and no cloud service.

## Verification status

Read this first, because it limits what the rest can claim.

- **Checked against the code in this repository:** every statement about how the agent, the panel and the shell work, with the file cited. Line numbers are as of `main` at a06695e.
- **Taken from documentation:** every statement about Spotify, Windows, Vosk, whisper.cpp, Android and the web platform, linked at the end. Where a page did not say something, the text says "not stated" and does not fill the gap.
- **Not verified on the device:** the Spot's microphone, the hardware mute switch, and anything about audio capture on the Spot. Nothing in this document was tried on the Spot.
- **Not verified on the PC:** every recogniser. Accuracy, latency, idle CPU, memory, and whether the Windows recogniser sends audio anywhere are all unmeasured. Section 7 is the plan to measure them before building.
- **Could not be read:** the XDA thread for the rook ROM returned HTTP 403 to my fetch, so the statement that the hardware mute switch does not work comes from the owner's brief, not from my reading of the thread. The Android reference pages for `RECORD_AUDIO` and `PermissionRequest` did not return their content either.

Device facts, measured by the owner: Echo Spot 2017 (rook), unofficial LineageOS 18.1, 480x480 at density 160, `MemTotal` 1958192 kB, `ro.config.low_ram=true`. `docs/device.md:3` says the device has 1GB, which disagrees with the measured `MemTotal` (about 1.9 GiB); that file should be corrected separately.

## 1. Microphone location

### Option A: the PC's microphone, captured by the agent

The agent already runs on the PC as a Windows tray app (`agent/cmd/spotdash/main.go`, `agent/internal/tray/run.go`). Capture and recognition happen in that process or a child of it. Nothing crosses the LAN except the result of a command, which uses the WebSocket the panel already has.

What it needs: a recogniser (section 2), a microphone the owner actually speaks near, and Windows letting desktop apps use the microphone. Microsoft documents a "Let desktop apps access your microphone" setting that applies to all desktop apps together (support article linked below), so a machine with that off gives the agent silence. How the recogniser reports that (an error or silent nothing) is not stated and needs testing.

### Option B: the Spot's microphone, captured by the shell

What the code and platform require, in full:

1. **Native capture in the shell.** The page cannot open the microphone itself. It is served over plain http from the agent's LAN address (`agent/internal/app/app.go:224`, the shell stores an address like `http://<desktop-ip>:8765`, `docs/device.md:101`, and sets `usesCleartextTraffic="true"`, `shell/app/src/main/AndroidManifest.xml:22`). MDN states that `getUserMedia()` is only available in secure contexts (HTTPS, `file:///`, or a page loaded from `localhost`) and that in an insecure context `navigator.mediaDevices` is `undefined`. A LAN-address page is not any of those. The `adb reverse` route in `docs/device.md:78-86` gives a `127.0.0.1` address for USB testing only; MDN's page names `localhost` and does not say whether `127.0.0.1` counts.
2. **Even in a secure context, the shell would have to say yes.** The shell's `WebChromeClient` overrides only `onConsoleMessage` (`shell/app/src/main/java/dev/spotdash/shell/PanelActivity.kt:210-219`). The Android source's own Javadoc for `onPermissionRequest` says "If this method isn't overridden, the permission is denied." Native capture in Kotlin sidesteps this entirely, which is why it is the realistic route.
3. **`RECORD_AUDIO` in the manifest.** The manifest declares INTERNET, ACCESS_NETWORK_STATE, WAKE_LOCK and WRITE_SETTINGS only (`AndroidManifest.xml:4-14`). Android's permissions overview says runtime ("dangerous") permissions must be requested at runtime, that the user can deny, and that an app must not assume they were granted. I could not read the `RECORD_AUDIO` reference page itself, so its protection level is not confirmed from documentation. The shell has no screen for prompting; `docs/device.md:147` shows the existing pattern of granting through adb.
4. **Audio transport to the agent.** There is no path for it. The WebSocket is one way: the server drains and discards anything the panel sends (`agent/internal/server/ws.go:68-74`, `conn.CloseRead`). The shell's JavaScript bridge has exactly four methods, all about the display (`ShellBridge.kt:14-24`). A new authenticated endpoint accepting an audio stream would be needed on the agent, and a capture-and-stream service in the shell. `docs/architecture.md:336` lists "any write path from UI to agent" as out of scope, though that line is already out of date (see section 6).
5. **A recogniser on the far end anyway.** Streaming raw audio over the LAN to the agent does not remove the need for section 2; it adds a network hop and a transport to secure.

What is unverified about the Spot: `docs/device.md:14` lists the microphone among the experimental parts of the ROM ("speaker, Bluetooth, camera, mic, sensors all experimental") and says spotdash only touches display and network. The owner's brief says the hardware mute switch is listed as not working. If that holds, an always-open microphone on the Spot has no physical off switch, which is a privacy problem on its own and rules out the "mute switch is my guarantee" argument. Whether the microphone captures anything usable on this ROM, at what quality, and from what distance, is unknown.

### Recommendation

**The PC's microphone, captured by the agent.** It needs no shell change, no manifest change, no new network path and no dependence on an experimental part of the ROM. The Spot stays a display. Revisit the Spot's microphone only if the PC microphone proves unusable from where the owner sits.

## 2. Recogniser

All three run without a network connection as far as their documentation says. None of the figures the owner asked for (idle CPU, latency, false-trigger rate, agent footprint) are stated by any of the documentation I read, so the table says "measure" rather than guessing. Section 7 measures them.

| | A. Windows built-in, phrase list | B. Vosk, restricted word list | C. Whisper, local |
| --- | --- | --- | --- |
| Where it runs | In-process engine from Windows (`SpeechRecognitionEngine`: "an in-process speech recognition engine") | Native library plus a model file | Native library plus a model file |
| New dependency in the Go module | None for the helper-process route. `go-ole` for the COM route, already an indirect dependency (`agent/go.mod:16`) | A Go binding or a hand-written binding to `libvosk`; the docs I read do not say the Go binding is cgo-free | The Go bindings, which per the whisper.cpp docs need cgo and a pre-built `libwhisper.a` |
| Size added to the download | None (the recogniser is part of Windows). The helper script is a few kilobytes | The library, plus a model: `vosk-model-small-en-us-0.15` is listed at 40M | The library, plus a model: tiny 75 MiB on disk and about 273 MB of memory, base 142 MiB and about 388 MB, small 466 MiB and about 852 MB |
| Agent footprint, idle CPU, latency | Not stated. Measure | Not stated. Measure | Not stated. The docs' streaming example transcribes audio "every half a second". Measure |
| False-trigger behaviour | Grammar-constrained. Each result carries a `Confidence`; the docs say it is relative, not an absolute probability, so a threshold has to be tuned and rejected input is reported through `SpeechRecognitionRejected`. Measure | Grammar-constrained via `vosk_recognizer_new_grm`; the header says it "might return [unk] if user said something different", which gives an explicit reject result. Measure | Not stated. The docs I read describe free-form transcription and do not say how it behaves on silence or noise. Measure |
| Licence | Part of Windows; nothing is redistributed. No new licence enters the repository | Apache-2.0 for the toolkit; the small English model is listed as Apache 2.0 | MIT for the toolkit. Model licence not stated on the page I read |
| Builds with `CGO_ENABLED=0` | Yes, on the helper-process route. The COM route would use `go-ole`, which I have not tried | Depends on the binding, see below | No, per the docs the Go bindings need cgo |

The release workflow builds with `CGO_ENABLED: "0"` on Linux and cross-compiles (`.github/workflows/release.yml:21-28`), and `agent/internal/sources/telemetry/gpu_windows.go:12-20` records that avoiding cgo was a deliberate choice. Anything that needs cgo changes how releases are built, which is a real cost and not a detail.

### A. Windows built-in speech recognition with a constrained phrase list

Documented facts: `SpeechRecognitionEngine` is an in-process recogniser; grammars are loaded with `LoadGrammar`; input is set with `SetInputToDefaultAudioDevice`; `SpeechRecognized` fires when input matches a loaded grammar and `SpeechRecognitionRejected` when it matches none; `SetInputToNull` disables input; the class exists on .NET Framework 3.0 through 4.8.1 and on current .NET through the `System.Speech` package. The docs also describe a separate WinRT route, `SpeechRecognitionListConstraint`, and state that recognition with a custom constraint "is performed on the device"; it is not pursued because there is no documented path to WinRT from Go.

How it would be called from Go on Windows, two ways:

1. **Helper process (the lean).** The agent starts `powershell.exe` as a child and reads one line per recognised command from its standard output. Microsoft's PowerShell documentation says Windows PowerShell 5.1 is built on the .NET Framework, where `System.Speech` is available in the box, so no package is installed. The script builds a `GrammarBuilder` from the phrase list, loads it, sets the default audio device as input and calls `RecognizeAsync(Multiple)`. The agent side is `os/exec` and a line reader, so the module gains no dependency. Costs: a resident `powershell.exe` process (its memory is unmeasured), a dependency on Windows PowerShell 5.1 and on an installed English recogniser being present, and a risk that antivirus or script-block logging treats an embedded script badly. That risk is mine, not from documentation, and must be checked.
2. **SAPI 5 over COM.** The SAPI 5.4 overview describes `ISpRecoContext`, a shared recogniser (`CLSID_SpSharedRecoContext`) or an in-process one (`CLSID_SpInprocRecoInstance`), command-and-control grammars loaded with `ISpRecoGrammar::LoadCmd...`, and events delivered by window message, callback or Win32 event. Calling it from pure Go through `go-ole` would keep the release build unchanged, but I have not verified that SAPI's event delivery works through `go-ole`, and the overview page does not cover the automation (IDispatch) interfaces. Treat it as an option to prove, not a plan.

Two things to keep in mind. The shared recogniser is shared with other applications and, per the `Confidence` page, its thresholds live in a per-user profile in the registry that applications should not change; use the in-process engine instead. And nothing I read says whether this recogniser sends audio anywhere, or writes any to disk: the test in section 7 checks both.

### B. Vosk with a restricted word list

Documented facts: Apache-2.0, offline, small models around 50 MB, bindings listed for many languages including Go. The C header documents `vosk_recognizer_new_grm(model, sample_rate, grammar)` where the grammar is a JSON array of phrases, says this "will improve recognizer speed and accuracy but might return [unk] if user said something different", says only models with a lookahead graph support it, and says audio is 16-bit mono PCM. The Vosk install page does not cover Go or C.

How it would be called from Go on Windows: through a Go binding, or by loading `libvosk.dll` with `windows.NewLazyDLL` as `gpu_windows.go` does for `nvml.dll`. Two consequences are my analysis, not from the docs: that route uses `NewLazyDLL` with an absolute path because `NewLazySystemDLL` (used for NVML) only looks in the system directory, so the trust question of loading a DLL from next to the binary needs an answer; and Vosk does not capture audio, so the agent would also need its own Windows capture (waveIn or WASAPI), which is extra code or another dependency. Not verified: whether the Go binding needs cgo, and whether a prebuilt Windows `libvosk` is available. The Vosk pages I read do not say.

### C. Whisper run locally

Documented facts: MIT licence; models as in the table; Go bindings in `bindings/go` that need cgo, a C compiler and a pre-built `libwhisper.a`, with Darwin and Linux as the platforms listed for that build (Windows is not mentioned for the Go bindings, although the project as a whole supports Windows); a streaming example exists and needs SDL2; a voice-activity-detection feature is mentioned. Not stated: any way to restrict output to a phrase list, and how it behaves on silence.

How it would be called from Go on Windows: cgo bindings, which breaks the `CGO_ENABLED=0` release build, or a separate `whisper.cpp` executable run as a child process. Either way the agent would feed it audio and match the transcript against the command list itself. This is the most flexible option and the heaviest; it only makes sense if free-form queries are wanted later.

### Recommendation

**A, via the helper process.** Fallback **B** if A fails the test. **C** only if free-form queries are wanted later. This is a lean to be confirmed by the section 7 numbers, not a conclusion: nothing about A's accuracy on the owner's voice is known.

## 3. Trigger

### Spoken prefix inside the grammar

Every command starts with a prefix, for example "spot, next face". The microphone is open all the time and every utterance is matched against the grammar.

- Idle cost is continuous: the recogniser runs whenever the microphone is open. Measure it.
- During calls: everything said in the room reaches the recogniser. The owner's own speech on a call can contain "spot", "next" or "pause". With the PC speakers on, the other participants' voices bleed into a desk microphone. With a headset as the default microphone, remote voices no longer reach it, but the owner's speech into the headset still does, and the owner is no longer near the Spot.
- Headset as the active microphone: `SetInputToDefaultAudioDevice` follows "the default audio device". What happens when the default device changes while listening (headset connected or disconnected mid-session) is not stated and needs testing. Narrowband headset audio may lower accuracy; that is also unmeasured.
- Failure looks like a wrong action: a song pauses during a call, or the panel changes face.

### Global push-to-talk hotkey

Windows `RegisterHotKey` defines a system-wide hot key. With a null window handle, `WM_HOTKEY` is posted to the calling thread's message queue and must be handled in that thread's message loop. It fails if the key combination is already registered (Windows' own reserved ones can be overridden in some cases); `MOD_NOREPEAT` stops auto-repeat generating multiple notifications; F12 is reserved and must not be used. Go would call it through `golang.org/x/sys/windows`, which the agent already imports, from a goroutine locked to an OS thread that runs a message loop (my analysis; the repo has no message loop today).

- The recogniser is armed only for a short window after the key press (for example six seconds, or until one command is recognised). Outside the window `SetInputToNull` (or a stopped child) means no audio is processed. The page I read describes press notifications only, so the design uses a timed window, not hold-to-talk.
- During calls: nothing is matched unless the key is pressed, so the owner's call speech cannot trigger anything. The risk moves to a key combination colliding with a conferencing app's own shortcut; `RegisterHotKey` fails visibly if the combination is taken, and that failure must disable voice, not fall back to always listening.
- Headset: the same default-device question as above, but only during the armed window.
- Cost to the feature: it is no longer hands-free. Pressing a key and speaking is barely better than tapping the panel or pressing a media key, so the value is in "not moving the hand to the mouse" and "not looking at the panel".

### Recommendation

**The hotkey, for the first build.** It has the smallest privacy footprint (the microphone is processed only when asked), no idle cost outside the window, and a false-trigger risk bounded by the window. Test the prefix grammar in section 7 as well, because the numbers decide whether always-listening can be offered later as an opt-in `trigger` setting. Do not ship always-listening unless it passes the soak day in section 7.

## 4. Command mapping

The agent already has the actions for Spotify. It has no way to change the panel's face.

### Face changes: needs a new agent action and a panel change

How a face changes today: only in the panel's own JavaScript. Tap zones call `showFace` (`agent/web/app.js:463-487`, arrow keys at `479-486`), and one source-driven path exists: `handleCalendarUrgency` switches to the calendar face when a calendar reading says an event is urgent, holds it, then returns (`app.js:177-217`, dispatched from the socket message handler at `app.js:411-413`). The agent cannot command the panel directly: the WebSocket is agent-to-panel only and incoming frames are discarded (`ws.go:68-74`).

So a voice face change follows the calendar precedent. The voice source publishes a reading through the existing store and WebSocket, and `app.js` gains a handler next to the calendar one. The message shape stays `{source, ts, data}` (`ws.go:31-35`).

Two details that would otherwise cause bugs:

- **Snapshot replay.** On every (re)connect the server sends the latest reading of every source that has one (`ws.go:127-139`). A stored "next face" would replay each time the panel reconnects or the page reloads. The reading therefore carries a sequence number and the agent's timestamp; the panel applies a command only if its sequence is new and it is fresh (a few seconds old at most, using `state.clockOffsetMs`, `app.js:69`, `276-282`).
- **Hidden faces.** `hidden_faces` removes faces from the panel's list (`app.js:33-37`, validated against `KnownFaces` in `agent/internal/config/config.go:37`). "Show status" when status is hidden finds no index. It should do nothing on the panel and be logged, not switch to something unexpected.

Face names come from `config.KnownFaces` (`clock`, `calendar`, `spotify`, `telemetry`, `status`). The initial phrase list covers three of them plus next and previous; the others are reachable by stepping.

### Spotify commands: existing actions, one small extraction

| Phrase | Action | Where it lives |
| --- | --- | --- |
| "pause" | `pause` | `PUT /v1/me/player/pause`, `api.go:154-156`; selected in `handleControl`, `apisource.go:237-238` |
| "stop" | `pause` | Same. The Web API reference pages I read list pause and start/resume and no stop endpoint |
| "play" | `resume` | `PUT /v1/me/player/play`, `api.go:158-160`; `apisource.go:239-240` |
| "next track" | `next` | `POST /v1/me/player/next`, `api.go:162-164`; `apisource.go:241-242` |
| "previous track" | `previous` | `POST /v1/me/player/previous`, `api.go:166-168`; `apisource.go:243-244` |

These four are exactly what the code allows on purpose: `playbackController` has these four methods and no others (`apisource.go:27-37`). Voice keeps that surface. It must not gain volume, seek, shuffle or queueing.

The gap: the switch and everything after it (the `expectingChange` flag for skips and the immediate repoll, `apisource.go:263-277`) lives inside an HTTP handler, `handleControl` (`apisource.go:223-280`), and the route exists only in `api` mode; the `mock` source has no routes (`spotify.go:188-193`). A voice source cannot call a handler on another source. Two ways to close the gap:

1. Extract the body of `handleControl` into a method (`Control(ctx, action) error`) that both the handler and voice call, and expose it through a small optional interface, the same pattern as `RepollRegistrar` (`agent/internal/sources/registry.go:56-63`). No behaviour change to the existing route. This is the recommendation.
2. Have voice call `POST /spotify/control` over loopback with the bearer token. It works with no code change to the Spotify source, but the agent then calls itself over HTTP holding its own token, which is worse than a function call.

In `mock` mode, or with Spotify disabled, a playback command finds no controller and is reported as such (below).

### OAuth scopes and the one-time re-consent

- **Requested today:** `user-read-currently-playing user-read-playback-state user-modify-playback-state` (`agent/internal/sources/spotify/auth.go:26`, sent in `beginAuth`, `auth.go:112-136`).
- **What playback control needs:** `user-modify-playback-state`. The Web API's pause reference lists it as the required scope, says the endpoint only works for Premium users, and the scopes page lists pause, resume and skip under it. The account is Premium.
- **So nothing new is requested.** `user-modify-playback-state` has been in the scope string since the transport controls were added (`5cfd4a4`, #35), and the comment at `auth.go:23-25` records why an older connection lacks it.
- **Where re-consent applies:** a connection made before that commit. The stored state holds only a refresh token (`store.go:14-16`), the token response type does not read a `scope` field (`token.go:44-51`), and the agent therefore cannot tell which scopes its stored token has. The code comment asserts an old grant keeps old scopes; Spotify's refreshing-tokens page does not state what a refreshed token's scopes are, so the design treats the owner's statement as the working assumption and does not claim it from documentation.
- **How to tell:** send one `pause` while something is playing. A failure with `HTTP 403` and no `PREMIUM_REQUIRED` message points at scope. The code cannot say so: an unrecognised 403 falls through to a generic message (`api.go:204`) and `handleControl` returns it as 502 (`apisource.go:251-259`), and the API-calls page does not document an insufficient-scope response. The build should add a hint to that message in the style of `connectionError` (`apisource.go:426`).
- **How to re-consent, once:** with the agent running, open `http://127.0.0.1:8765/spotify/connect?token=<token from config.json>` in a browser. The route sits behind the bearer token, which the server accepts as `?token=` (`server.go:195`). It redirects to Spotify's consent page (`apisource.go:202-208`); after approval the callback stores the new refresh token in `state_file` (`auth.go:173`) and asks for an immediate poll. Whether Spotify shows the consent screen again for an already-connected account is not stated in the pages I read.

### "No active device", visibly

The failure path already exists in the agent: Spotify's error `reason` `NO_ACTIVE_DEVICE` maps to `errNoActiveDevice` (`api.go:198-200`, `26-34`) and comes back as 409 (`apisource.go:254-255`). Note that the Spotify pages I read do not document that reason or a 404 for these endpoints; the mapping rests on observed behaviour recorded in the code, so the build should keep a test for it.

What is missing is somewhere to show it. The panel's transport buttons flash a 600 ms denied state on a failed tap, and the code comment says "there is nowhere else on the panel a one-off error like this can show up" (`agent/web/faces/spotify.js:146-152`). Those buttons are also hidden when nothing is playing (`spotify.js:269-270`), which is exactly the no-device situation. A spoken command has no button to flash.

Design: the voice reading carries the last command's result (`ok` and a short fixed message, such as "no active spotify device" or "spotify not connected"). The panel gets one small notice element in `app.js`, not inside any face, shown on whichever face is up for about four seconds. Success shows nothing beyond its own effect. It is not a source failure, so it does not mark the source degraded; a command that cannot run is not a broken microphone. The notice text is chosen from a fixed set, never a transcript.

## 5. Fit with the source model

This is an event-driven input. The source model is polled: `Source` is `Name`, `Poll`, `Interval` (`registry.go:24-28`), the runner calls `Poll` on a timer (`runner.go:88-124`), and only the runner writes to the store (`runner.go:164`; `state.Store.Update` is not handed to sources).

### Where it lives

**A source named `voice`, plus one small optional interface.** The interface, in the style of `RepollRegistrar` and `RouteProvider`, would be something like an event runner: the app calls `Run(ctx, env)` after the runner exists (`app.go:284-297` is where the repoll wiring is done today), and `env` gives the source a publish function backed by `store.Update`, and the playback controller found by type assertion (section 4). The source still implements `Poll` cheaply, returning the current state (listening, muted, last command result), at a slow interval, so it satisfies the interface and the config rule that an enabled source has a positive `interval_ms` (`config.go:266-270`). Commands are published the moment they happen rather than waiting for a tick.

Why a source and not a service outside `sources`:

- It gets `/health` and the status face without new code. `App.startSession` registers every configured source in the store (`app.go:213-220`), `handleHealth` reports each (`server.go:147-174`), and the status face lists every source `/health` reports (`agent/web/faces/status.js:55`, `98-121`).
- Config is uniform. Anything under `sources` is decoded by the source itself (`config.go:51-82`), so no change to the top-level `Config` struct, which rejects unknown keys (`config.go:149`). An unknown source name is already a startup error (`registry.go:103-106`).
- Reload works. A reload replaces the whole session, sources included (`app.go:39-41`, `108-111`), so a bad voice config is rejected before anything running is touched (`app.go:103-106`).

The alternative, a package owned directly by `App`, is simpler on the dependency graph but re-implements registration, status reporting and validation by hand. Its one real advantage is that it is not shaped like a poll. The cost of the source route is the new optional interface, and that this is the first source that acts on another source; the app wiring, not the source, does that lookup.

Consequences to design for, not discover later:

- A reload tears the microphone down and starts it again, and the settings page triggers a reload on every save (`app.go:372-376`). Mute state therefore belongs to `App`, not to the source, so a reload cannot quietly re-open a muted microphone.
- The runner recovers panics inside `Poll` (`runner.go:137-145`), but an event goroutine is not covered by that. The voice goroutine must recover its own panics and fail closed.
- The store keeps only the latest reading per source (`state.go:67-82`); see the replay handling in section 4.

### What appears in `/health` and on the status face

`sources.voice` with `status` `ok` while the recogniser is running (including while muted, which is the owner's choice and not a fault), `degraded` with `last_error` when the recogniser or microphone failed, and `disabled` when `enabled` is false (`app.go:214-219`). The status face shows it as one more row with the same status text and age as the others; its rim gains one segment (`status.js:62-85`). `/health` is unauthenticated and carries only status, age and error text (`server.go:127-131`), so it must never carry a phrase or a heard word. Error text is fixed messages such as "no recogniser installed".

### How it is configured

A `sources.voice` block in `config.json`, off by default. Sketch, not a commitment:

```json
"voice": {
  "enabled": false,
  "interval_ms": 5000,
  "recogniser": "windows",
  "trigger": "hotkey",
  "hotkey": "ctrl+alt+space",
  "listen_seconds": 6,
  "dry_run": false
}
```

`interval_ms` is the state refresh, not a listening interval. A confidence threshold is deliberately absent: its value comes from the section 7 test. `dry_run` recognises and logs a mapped command name without acting, for the soak day.

The tray gets an optional voice item set, the way "Start with Windows" is optional (`agent/internal/tray/tray.go:32-34`): a mute checkbox and a listening indicator. Today the tray only sets one icon and tooltip at startup (`run.go:35-37`), so a listening indicator needs a second icon asset and a tooltip change. That is new tray work, not a tweak.

## 6. Security and privacy

What changes: **the agent opens the microphone.** Until now the agent has read a calendar feed, a Spotify account and the machine's own counters. It has never captured audio.

- **Off by default.** The example config ships `enabled: false`. The microphone is opened only when the owner sets `enabled: true` in `config.json`; that edit is the explicit switch. No settings-page toggle enables it.
- **Tray indicator while listening.** A distinct tray icon and tooltip while the microphone is open, set by the same code that opens it, so the indicator cannot disagree with the state. Microsoft documents a microphone-in-use icon in the taskbar's notification area; whether it appears for this process is not stated for desktop apps and is not relied on.
- **Tray mute toggle.** One click closes the microphone (the recogniser is stopped, not just ignored). It survives a reload (section 5) and resets to unmuted only when the agent restarts with voice enabled.
- **Nothing recorded to disk.** No audio file, no cache, no transcript file. The design asks the recogniser for text only and never touches the audio the result object exposes. The test in section 7 checks that nothing else writes audio: nothing I read says.
- **Nothing sent off the PC.** No network call is part of recognition. For the built-in recogniser the documentation says "in-process" but does not say that no audio leaves the machine; the test runs it with the network disabled and watches for connections.
- **Recognised phrases at debug level only.** The command name from the closed list is logged at debug, never at info. Be precise about what that means: the log is a file next to the config, rotated with 3 backups and a 28-day age limit (`agent/internal/logging/logging.go:56-67`), so at `log_level: "debug"` command names persist on disk for up to that long. Info level records only that voice started, stopped or failed, and counts.
- **Fail closed.** If the recogniser fails to start, the helper process exits, the microphone cannot be opened or the hotkey cannot be registered, voice stops: the child is killed, the microphone is closed, the source reports `degraded` with a fixed reason, and nothing retries by itself. It stays that way until a reload or an unmute. The rest of the agent is unaffected: sources, the server and the panel do not depend on it, and a failed construction is caught by reload validation before anything running is touched (`app.go:103-106`). A failed hotkey registration must not fall back to always listening.
- **No new network surface.** Commands go from the recogniser to existing in-process actions. There is no new HTTP route and the WebSocket carries only a command name, a sequence number, a timestamp and a fixed result message, never audio or a transcript.
- **Voice is not authentication.** Anyone in earshot can issue a command. The limit is the closed list: pause, resume, skip and face changes. Nothing destructive, and no free-form text ever reaches an action.
- **Already stale in the security notes.** `docs/architecture.md:336` says there are "No command endpoints, so a leaked token only reads". `POST /spotify/control` (`apisource.go:210`, `spotify.go:44`) already changes playback, so a leaked token can pause the music today. Voice does not change that, but the security section should be corrected when this is built. Not touched in this PR.

## 7. Accuracy test plan (throwaway, before building)

Purpose: decide whether A is good enough and whether always-listening is even worth considering. Scripts live in a scratch directory outside the repository and are deleted afterwards. About ten minutes per option, on the owner's PC, in the owner's room, with the microphone the owner will really use, at the normal distance.

**Setup.** Write each option's harness so that it recognises against the same ten phrases and prints a timestamped line per recognition (and, for a rejected utterance, a line saying so). The ten phrases: next face, previous face, show calendar, show spotify, show clock, pause, stop, play, next track, previous track.

- A: a PowerShell 5.1 script using `SpeechRecognitionEngine` with `SetInputToDefaultAudioDevice`, a `GrammarBuilder` from the list, and `SpeechRecognized` and `SpeechRecognitionRejected` handlers that print the phrase, `Confidence` and the alternates.
- B: any harness that feeds the default microphone at 16 kHz mono to a grammar-restricted recogniser with the same list plus `[unk]`.
- C: any harness that transcribes short windows and does exact matching of the transcript against the list.

**Part 1, commands (about five minutes).** Say each phrase ten times, 100 utterances in a shuffled order (a printed list), at normal pace, a steady pause between each. Run it twice in the same session: once as the bare phrase (the hotkey case) and, if time allows, once with the prefix "spot," ahead of each (the always-listening case). Record per utterance: correct command, no result, or a different command.

**Part 2, normal speech (five minutes).** Three minutes reading a prepared paragraph aloud, written to contain ordinary uses of "next", "stop", "play", "pause", "show", "spot" and "face" inside sentences. Then two minutes of audio playing from the PC speakers (a video call or a podcast) with the owner silent, to test bleed. Count every command that fires. If the owner uses a headset, repeat Part 2 with the headset as the default device as an optional second pass, and note what happens when the default device is switched while the harness runs.

**Also measure, while it runs:**

- *Latency:* from the end of the utterance to the printed line. The owner taps a key as they finish each phrase; the harness logs the difference. Human error is a couple of tenths of a second, which is fine for the pass mark below.
- *Idle CPU and memory:* one minute of a quiet room with the recogniser armed, average CPU and working set of the recogniser process (and the agent) from Task Manager or performance counters.
- *Privacy (A only):* run Part 1 again with the network adapter disabled (results should not change), watch for outbound connections from the process during it, and use a file-activity monitor to confirm nothing writes audio. Check the "Let desktop apps access your microphone" setting turned off to see how the recogniser fails.

**Pass mark** (proposed; the owner can change the numbers):

| Measure | Pass |
| --- | --- |
| Correct command, bare phrases | at least 95 of 100 |
| Wrong action (a different command fired) | at most 1 of 100. A wrong action is worse than a miss |
| False triggers in Part 2 | zero, including the two minutes of speaker audio |
| Latency, end of utterance to result | median at most 1.0 s, worst at most 2.0 s |
| Idle CPU, armed and quiet | average at most 2 percent of total CPU; working set at most 150 MB |
| Privacy (A) | no changed result offline, no outbound connection, no audio written |

**What this test cannot show.** Zero false triggers in five minutes says only that an option is not obviously bad; by the usual rule of three the upper bound on the rate is still about three per five minutes. The real false-trigger measure is a soak: one ordinary working day with `dry_run` on and `log_level: "debug"`, counting mapped commands that fired with nothing intended. Only an option that passes the five-minute test earns the soak, and always-listening is offered only if the soak shows none that matter.

**Decision.** If A passes, build A. If A fails and B passes, build B and accept the binding and capture work. If only C passes, that is a signal free-form is wanted, not a reason to ship C for ten phrases.

## 8. Open questions for the owner

Each has a recommendation, so a short answer unblocks the build.

1. **Microphone: the PC's?** Recommend yes, and say which physical microphone (desk, headset or webcam) is the one to test.
2. **Trigger: hotkey first, prefix later only if it passes the soak?** Recommend yes. If so, which key combination? Recommend one that does not collide with a conferencing app's shortcuts, not F12.
3. **Initial command list as in section 4?** Recommend yes as it stands. Say if any phrase should change or be dropped.
4. **Helper process running Windows PowerShell 5.1 for recognition?** Recommend yes. It keeps `CGO_ENABLED=0` and adds no module. It does make Windows PowerShell 5.1 and an installed English recogniser a requirement for voice, and voice is off unless enabled.
5. **Pass marks in section 7 acceptable?** Recommend yes, adjusting the CPU and latency numbers if the owner cares about them differently.
6. **Run the soak day in `dry_run` before turning actions on?** Recommend yes; it costs one config line.
7. **Re-consent to Spotify now, before the build?** Recommend yes: one visit to `/spotify/connect` and a test `pause`. It removes the only Spotify unknown.
8. **Rewrite issue #73's title and body to match this design?** Recommend yes. The current text asks for a hosted service, an API key in `config.json` and an outbound connection; none of that applies. Suggested title: "Voice source: local voice commands". Not done here, because it is an outward-facing change to the issue.
9. **Mute state: kept across a config reload, reset on agent restart?** Recommend yes; it fails toward a closed microphone.

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
