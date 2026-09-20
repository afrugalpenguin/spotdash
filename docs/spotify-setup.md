# Rolling your own Spotify app

Every spotdash setup needs its own Spotify app. There is no shared one to
point at: an app can only serve a handful of people (see "New apps and
Development Mode" below), so one app can't cover everyone who uses this.
Auth is Authorization Code with PKCE and no client secret, so anyone can
create their own Spotify app and point their own `config.json` at it. Takes
about five minutes.

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

A freshly created app starts in Development Mode. As of September 2026,
Spotify's rules for it are:

- Up to five authorised users per Client ID, each added by you under
  **Users Management** in the dashboard before they connect.
- The account that owns the app must have Spotify Premium.

That's not a problem here - you're the only person using it, so add the
Spotify account you're actually going to connect with under **Users
Management** before hitting `/spotify/connect`.

Separately, pause, resume, next and previous need Premium on the connected
account. Spotify refuses them otherwise, and the panel briefly turns the
button red.

There is no way to lift the five user cap for a project like this.
Extended Quota Mode is only open to registered organisations with at least
250,000 monthly active users, and individuals aren't accepted. That is why
each person creates their own app rather than sharing one.

The current rules are on Spotify's side, so check them if this looks out of
date: [quota modes](https://developer.spotify.com/documentation/web-api/concepts/quota-modes)
and the [February 2026 Development Mode changes](https://developer.spotify.com/blog/2026-02-06-update-on-developer-access-and-platform-security).
