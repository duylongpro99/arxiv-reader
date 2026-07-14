# X (Twitter) channel — one-time OAuth2 setup

The `x` channel (`backend/internal/channels/x`) publishes a `brief` as a
numbered tweet thread via X API v2, which requires **OAuth2 user-context**
auth — not a static API key like dev.to. This is a one-time, human-run setup;
the agent never performs it automatically (per this repo's rule against
agents handling auth flows or secrets on your behalf).

## 1. Register an app on the X developer portal

1. Go to https://developer.x.com/en/portal/dashboard and create (or reuse) a
   Project + App.
2. Under the app's **User authentication settings**, enable **OAuth 2.0**.
3. App type: **Web App, Automated App or Bot** (confidential client — this
   channel authenticates with `client_id` + `client_secret`, not PKCE alone).
4. Scopes: enable `tweet.write` and `offline.access` (the latter is what
   makes X issue a `refresh_token` at all).
5. Add a callback/redirect URL. Since this is a one-time local exchange, any
   reachable localhost URL works, e.g. `http://127.0.0.1:8765/callback`.
6. Save the app's **Client ID** and **Client Secret**.

## 2. Run the Authorization Code + PKCE flow once

This step happens outside the running server — it's a one-time exchange to
get a `refresh_token`, done with any OAuth2 PKCE helper (e.g. `curl` + a
manual code_verifier, or a small local script/Postman). It is NOT part of
this codebase (no PKCE helper is shipped, and none should be committed —
it would need to run interactively in a browser).

1. Generate a random `code_verifier` (43-128 chars) and its
   `code_challenge` (`BASE64URL(SHA256(code_verifier))`).
2. Send the user's browser to:
   ```
   https://twitter.com/i/oauth2/authorize
     ?response_type=code
     &client_id=<CLIENT_ID>
     &redirect_uri=<your redirect URL>
     &scope=tweet.write%20offline.access%20users.read
     &state=<random>
     &code_challenge=<code_challenge>
     &code_challenge_method=S256
   ```
3. Approve the app; X redirects to your callback URL with `?code=...`.
4. Exchange the code for tokens:
   ```
   curl -u "<CLIENT_ID>:<CLIENT_SECRET>" \
     -d "grant_type=authorization_code" \
     -d "code=<code from step 3>" \
     -d "redirect_uri=<same redirect URL>" \
     -d "code_verifier=<code_verifier from step 1>" \
     https://api.twitter.com/2/oauth2/token
   ```
5. The response contains `access_token` (short-lived, ignore it — the
   channel fetches its own) and, crucially, `refresh_token`.

## 3. Configure `.env`

```
X_CLIENT_ID=<from step 1>
X_CLIENT_SECRET=<from step 1>
X_REFRESH_TOKEN=<refresh_token from step 2.5>
```

Optional: `X_TOKEN_STORE=/absolute/path/to/local/file` — X **rotates** the
refresh token on every use. The channel keeps the rotated token in memory for
the life of the process; setting `X_TOKEN_STORE` also best-effort persists it
to a local file (mode `0600`) so a server restart doesn't fall back to the
now-stale value in `.env`. Never commit this file — keep it outside the repo
or add it to `.gitignore` if it must live inside.

## 4. Enable the channel

Add `"x"` to `publishing.channels` in `config.yaml` (already whitelisted by
`internal/config`). Restart the server; a missing/invalid credential fails
fast at startup with a named list of missing env vars, so a misconfigured `x`
channel never silently half-works.

## Known limits

- **Free tier write cap**: X's free API tier caps writes per app per month
  (historically very low, e.g. tens of posts). A thread of N tweets consumes
  N of that quota. Budget accordingly, or upgrade the developer plan.
- **Partial-thread failures**: if a tweet mid-thread fails to post, earlier
  tweets in that thread are already live and are **not** auto-deleted — the
  publish error names which segment failed so it can be retried or handled
  manually.
- If this OAuth setup is skipped entirely, the `x` channel simply isn't
  registered as usable (`New` returns an error) — dev.to and other channels
  are unaffected.
