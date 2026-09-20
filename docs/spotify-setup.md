# Rolling your own Spotify app

The shared spotdash Spotify app is capped at whatever Spotify's Development
Mode allows (or Extended Quota Mode, if granted - see the repo issues for
where that stands). If you'd rather not depend on it, or just want your own
account fully under your own control, auth is Authorization Code with PKCE
and no client secret, so anyone can create their own Spotify app and point
their own `config.json` at it. Takes about five minutes.

## 1. Create the app

1. Go to <https://developer.spotify.com/dashboard> and log in.
2. **Create app**. Name and description can be anything.
3. Under **Redirect URIs**, add the callback URL exactly as your agent will
   serve it: `http://127.0.0.1:8765/spotify/callback` if you're running the
   agent locally with the default port, otherwise match whatever `listen`
   you've set in `config.json`. This has to match byte-for-byte or the OAuth
   handshake fails.
4. Tick **Web API** under the APIs your app uses.
5. Save, then open the app and copy the **Client ID** from the dashboard.
   No client secret needed - PKCE doesn't use one.

## 2. Wire it into config.json

```json
"spotify": {
  "enabled": true,
  "interval_ms": 5000,
  "mode": "api",
  "client_id": "the client id you just copied",
  "redirect_uri": "http://127.0.0.1:8765/spotify/callback",
  "state_file": "spotify_state.json",
  "layout": "fill"
}
```

`state_file` is where the agent writes your refresh token once you connect;
it's gitignored, same as `config.json`. A relative path like this one is
relative to the folder `config.json` is in, wherever the agent is started
from, so it still finds your connection when started at login.

## 3. Connect

Start the agent, then visit `http://<agent host>:<port>/spotify/connect`
(token as `?token=`, or already set from the panel). Completing consent lands
on a page that says **Connected**.

## New apps and Development Mode

A freshly created app starts in Development Mode with a 25-user allowlist.
That's not a problem here - you're the only person using it, so under
**Users Management** in the dashboard, add the Spotify account you're
actually going to connect with, before hitting `/spotify/connect`. You don't
need Extended Quota Mode for a personal setup.
