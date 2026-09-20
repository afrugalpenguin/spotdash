# Voice source: design

Proposal for #73. Nothing is built. The issue asked for a design pass first, so this lays out what "voice" could mean, what fits the code, what changes about security, and the decisions that are yours. Last section is the list of questions.

Facts about OpenAI's APIs are from their docs as read in September 2026 (linked at the end). Things those pages did not state are marked, and worth checking before building.

## Three things "voice" could mean

| | Speak only | Push to talk | Wake word |
| --- | --- | --- | --- |
| What it does | Panel says the next event or an alert out loud | Hold something, ask a question, hear an answer | Always listening, answers after a spoken phrase |
| Audio out of the LAN | No, text only | Only while held | Only after the wake phrase, but the mic is always open |
| Needs the mic | No | Yes | Yes, continuously |
| New dependency | None | Native mic capture in the shell | A wake word engine on a 1 GB `low_ram` device |
| API shape | Request and response | Persistent stream per press | Persistent stream |
| Security change | Small | Real | Large |

## Recommendation: speak only first

Build the smallest useful thing, and find out on the real hardware whether the Spot can do the rest before designing for it.

- **Speak only** needs no mic, no persistent connection and no new permission. It is one HTTPS request per announcement.
- **Push to talk** is a second phase, decided after the speaker and mic are both proven on the device (see "If listening is wanted later").
- **Wake word** is not recommended: continuous capture and an engine on a low memory SoC, for a desk toy.

## Does it fit the source model?

The source contract is pull: `Poll(ctx)` on an interval, run by `sources.Runner`, one goroutine per source. That splits the two API styles cleanly:

- **Text to speech** (`/v1/audio/speech`) is request and response. It fits, but its trigger is an event ("the meeting became urgent"), not a tick.
- **Realtime** is a persistent, bidirectional stream over WebRTC or WebSocket, holding conversation state. It does not fit a polling source. It would need its own long lived connection, and only push to talk or wake word need it.

So a poll on a timer is the wrong driver. Two ways to add an event driven component:

1. **A service outside `sources`**, subscribing to `state.Store` (which already has `Subscribe`, used by the WebSocket hub) and publishing its result back with `store.Update("voice", ...)`. Needs its own config home, since `sources` names are validated against the factory table.
2. **A source named `voice`** so config, `/health` and the status face are uniform, with a new optional interface (in the style of `RepollRegistrar`) that hands it the store and lets it run its own loop. `Poll` returns the last utterance, so a panel that connects mid session gets the snapshot like any other source.

Recommend **2**. It matches "same pattern as Spotify and Calendar" from the issue, and the interface is one small addition. `interval_ms` is then only how often health is refreshed, and the docs should say it is not how often it speaks.

```
calendar reading (urgent, title, start_label)
        |  state.Store.Subscribe
        v
   voice source ---- text ---> api.openai.com/v1/audio/speech
        |                              |
        |  <---------- audio ----------+
        |  cache file next to config.json
        v
  store.Update("voice", {id, text, audio_url})
        |  existing WebSocket broadcast
        v
  panel: <audio src="/voice/audio/<id>"> plays it
```

Audio is fetched by the panel from the agent over the existing authenticated LAN route, so the key never reaches the device.

## What it announces

Default is one trigger: a calendar event becoming `urgent` (the server side flag, already computed from `notify_minutes`), once per event. The dedupe key is the same one the panel uses for its auto switch, title plus start label, so a reading that stays urgent does not repeat.

Other candidates, off by default: a telemetry alert, a status change to degraded. It stays silent during the clock's sleep window, read from the `sleep` field of the clock reading in the store.

## Config sketch

```json
"voice": {
  "enabled": false,
  "interval_ms": 60000,
  "mode": "speak",
  "api_key": "sk-...",
  "model": "gpt-4o-mini-tts",
  "voice": "marin",
  "say_titles": false,
  "announce": ["calendar_urgent"],
  "max_per_hour": 12
}
```

Fails closed like Spotify: no `api_key` is a startup error naming the key. `mode` is required with no default, as for spotify and calendar, so a half configured block can't silently do something. The example file would get a disabled block.

## Security and privacy changes

The current posture (docs/architecture.md) is a read only dashboard on a trusted LAN. Voice changes two things.

- **Data leaves the LAN.** Spotify and the ICS feed are outbound too, but they only fetch. This sends text derived from your calendar to api.openai.com. That is why `say_titles` defaults to false: the default line is "You have a meeting in 15 minutes", and event titles only go out if you turn it on.
- **A billing credential goes in `config.json`.** Same file and same gitignore as the LAN token, plaintext on disk. Rules: never logged, never in `/health`, the WebSocket or anything else the panel receives. `/settings` is safe by construction, since it only returns the four panel settings, not source blocks, and `config.Save` writes source blocks back verbatim. Use a dedicated key with a spend limit if the dashboard offers one. OpenAI's Realtime guide says API keys belong server side and clients get short lived tokens, which is another reason the device never holds one.
- **Listening would change more.** Push to talk sends live audio out while held, and a wake word means an open mic. If either is built, the security section of architecture.md needs rewriting, not a line added.

OpenAI's text to speech guide requires telling end users that the voice they hear is synthetic and not a human's. For a device on your own desk that is a README and setup doc sentence, and it should be there before the first release that includes this.

## Cost and abuse limits

Pricing and input length limits were not on the pages I read, so check them before building. Independent of price:

- `max_per_hour` cap, and a hard cap on characters per request.
- Cache audio by hash of the text and voice, so a repeated line costs one request. Cache directory sits next to `config.json`, resolved like other relative paths.
- One in flight request at most, with a timeout. On failure the source goes `degraded` and says why on the status face, and it does not retry in a loop: the next event is the next attempt.

## Testing

- A `Synthesizer` interface with a fake, and an `httptest` server standing in for the API, so no key is needed.
- Tests for: fires once per event, silent in the sleep window, `say_titles` off never sends the title, cap enforced, cache hit makes no request, and the key does not appear in logs, `/health` or `/settings`.

## Before building: prove the speaker

The whole feature rests on one thing about the device that is unproven: **speaker output**. docs/device.md lists the speaker as experimental on this ROM. The shell already sets `mediaPlaybackRequiresUserGesture = false`, so a page can play without a tap, but nobody has heard sound come out of the Spot. Check that first, with a test page and a short clip, and note the volume it comes out at. Announcing into a dark room at night is why the sleep window rule above exists.

## If listening is wanted later

What I found that constrains push to talk:

- The panel is served over plain `http://<lan-ip>`. Browsers only offer `getUserMedia` in secure contexts, so the WebView page cannot open the mic itself. This is the standard web platform rule and I have not tested it on the device. The likely answer is native capture in the shell: `RECORD_AUDIO` (not in `AndroidManifest.xml` today) and `AudioRecord`, streamed to the agent over the WebSocket, with the agent holding the Realtime connection and the key.
- The mic is also listed as experimental on this ROM.
- Touch: each half of the panel already switches faces and a 3 second press opens settings. It needs its own gesture or a dedicated control.
- The agent becomes a bridge between a device stream and a Realtime session, with per press setup and teardown, which is a different shape from anything in `internal/sources` today.

## Open questions

Each with what I would pick, so a one word answer is enough.

1. **Which interaction first?** Speak only.
2. **What does it announce?** Only an urgent calendar event.
3. **Are event titles allowed to leave the LAN?** No, generic phrasing by default, opt in with `say_titles`.
4. **Where should the key live?** In `config.json` like the token, or an `OPENAI_API_KEY` environment variable as well or instead? I would accept either, with `config.json` winning.
5. **Where should it play?** On the Spot, through the panel. The alternative is the PC's speakers, which needs no device audio at all and would be a way to start before the Spot's speaker is proven.
6. **What monthly spend is acceptable?** This sets `max_per_hour` and the character cap.
7. **Is a second phase (push to talk) wanted at all?** If not, the design stops at speak only and the Realtime material can be dropped.

## Sources

- Realtime API guide: https://developers.openai.com/api/docs/guides/realtime
- Text to speech guide: https://developers.openai.com/api/docs/guides/text-to-speech
