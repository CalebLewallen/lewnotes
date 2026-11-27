# S5b — Frictionless CLI login: the handoff flow

**Status:** implemented 2026-11-25 · live CLI check passed, manual browser pass pending · **Depends on:** S2 (auth), S5 (CLI + token page) · **Supersedes:** spec §11.4 (keep it as the fallback path)

**Goal:** `lewnotes login` requires zero secrets typed into the terminal. Every decision and every
piece of information that a human would otherwise type — server address, device name, credentials —
is entered or approved in the web app. The CLI ends up with a verified device token and the server
address, both carried in one opaque, encrypted handoff.

---

## 1. What we are changing, and why

Today the flow is: prompt for the server URL → open `/settings/tokens` → create a token → copy it →
paste it into a masked terminal prompt. That is three context switches and one secret through the
clipboard and terminal.

The desired shape:

```
$ lewnotes login
Opening https://notes.example.com/cli/authorize…
Waiting for approval in your browser…  (Ctrl-C to cancel)

✓ Authorized "caleb-laptop" as caleb on https://notes.example.com
✓ Saved credential to ~/.config/lewnotes/config.toml (0600)
```

One command, one browser tab, zero typing on a machine that already has a browser. Headless and
agent cases are covered without fallback typing (see §4.2 and §4.3).

Two things make this work:

1. **The web app owns the truth.** The owner configures the instance's public URL in the UI, so the
   address baked into tokens is correct even behind a tunnel or reverse proxy. The owner approves
   each device in a real browser session, with the device name visible and editable.
2. **The handoff is encrypted to the CLI, not to a key we ship.** The CLI generates an ephemeral
   X25519 keypair per login. The server encrypts the payload (server origin + token) to that public
   key. Only that running process can decrypt it. This is what the "encrypted token" idea should
   mean in practice.

### 1.1 Explicitly rejected: a static key compiled into the CLI

Encrypting the payload with a key that ships inside the `lewnotes` binary is security theater: anyone
who can read a captured token can also read the binary, extract the key, and decrypt it. It would add
ceremony without adding protection. The ephemeral-key design in §5 has the same ergonomics (one
opaque blob) and actually keeps the secret out of reach. This decision is worth recording because
"encrypt it into the token" is the natural first instinct.

### 1.2 What stays the same

- `lnt.<base64url(origin)>.<ulid>.<secret>` device-token format. Existing tokens keep working.
- `/settings/tokens`, the paste flow, `--token-stdin`, `--token`, `LEWNOTES_TOKEN`, `LEWNOTES_SERVER`.
  These are the break-glass, agent, and CI paths and must not regress.
- Exit codes. A denied or expired login is `1` with a machine-readable `error.code`, not a new code.

---

## 2. Threat model

| Threat | Mitigation |
|---|---|
| Handoff captured from a log, proxy, or terminal scrollback | It is ciphertext (`lnh1.…`) encrypted to an ephemeral public key that dies with the CLI process; single-use; 10-minute TTL |
| Handoff replayed | Row moves `approved → consumed` on first fetch; later fetches return `410` |
| Attacker opens the authorize page to mint a token | Requires an authenticated session; the page names the device and the requesting client; the resulting token is useless without the CLI's private key |
| Malicious or spoofed server returns a different origin to exfiltrate the token | CLI pins the origin it opened the request against and rejects a payload whose `server` differs; TLS verification stays on |
| Token leaks into logs / DB backups | Plaintext token exists only in process memory and inside the encrypted envelope; server stores `sha256(secret)` as today |
| Local attacker on the CLI host | Out of scope — they can read the 0600 config or ptrace the process |
| Loopback callback abuse (drive-by `GET`) | Callback carries only the request id, which is not a secret; the CLI still fetches with its own `state` header, and a wrong id never produces a handoff |
| Brute-forcing the user code | 40-bit code, 10-minute TTL, 5 attempts per request, per-IP rate limits |
| CSRF on approve/deny | Existing session + `X-Lewnotes-CSRF` requirement |

Non-goals: OAuth/OIDC/SAML, browser SSO into the CLI, OS keyring, multi-user roles, refresh tokens,
token scopes beyond `full`.

---

## 3. Public URL: configured in the app, not guessed

`server.public_url` exists in the server config but nothing surfaces it and nothing can set it at
runtime. It matters here because it is the origin baked into every token and every login URL.

**Resolution order (effective public URL):**

1. `server.public_url` from config / `LEWNOTES_PUBLIC_URL` — deployment intent wins; UI shows it read-only.
2. A new runtime setting stored in the DB (`settings` KV row `public_url`), editable in the UI.
3. Derived from the request (`originFromRequest`), used for dev and recorded with a warning.

**New: `/settings/instance`** (owner session required, linked from `/settings/tokens` and the sidebar
footer):

- Public URL field with live validation (absolute `http(s)` origin, no path/query/fragment/credentials), plus a **Test** action that pings `GET /api/v1/health` at the typed address and reports the version, instance name, and setup state before anything is saved.
- A "Use this browser's origin" button (`https://notes.example.com`) for the common case.
- Read-only display when the config file sets it: "Set by `server.public_url` in config.toml".
- Warnings when unset ("Tokens and login links will use whatever host the request arrived on — set
  this before using the CLI from another machine") and when it differs from the current origin
  ("You are browsing notes.local; tokens will point at notes.example.com").
- Read-only rows for `auth.trust_proxy` and `server.listen` so misconfiguration is diagnosable.
- The token page shows the same line: "Tokens are issued for https://notes.example.com".

**Endpoints**

```
GET   /api/v1/settings        → {public_url, source: "config"|"settings"|"derived", derived_origin, trust_proxy, listen}
PATCH /api/v1/settings        → {public_url}  409 settings_from_config if source == "config"
POST  /api/v1/settings/validate
  session + CSRF · 10/min/session
  → {url} → {valid, url, version, instance_name, setup_required, latency_ms}
  ← 400 invalid_public_url · 502 unreachable
```

The validation request is a plain server-side `GET <url>/api/v1/health` with a 5s timeout and no
redirect following, followed by `GET <url>/api/v1/auth/status` (optional, for the instance name). It
is owner-only and explicit; a homelab address is usually private, so blocking private ranges would
defeat the purpose.

Storage: `settings(key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL)` in
migration `0004`. `PATCH` also rewrites nothing on disk; config.toml stays operator-owned.

Why not write config.toml from the UI: comments and formatting are operator-owned, and a web process
rewriting its own config file is a footgun during upgrades. A DB setting is sufficient and clear.

---

## 4. The three flows

### 4.0 Bootstrap: the server address is the one thing the CLI cannot know

This is worth stating plainly, because it is the only irreducible input in the whole design.

A token can carry the origin because the server mints it. A handoff can carry the origin because the
server encrypts it. But **before the first login there is no token and no handoff** — there is only a
CLI binary on a machine that has never spoken to this instance. A self-hosted server has no canonical
address that a client could look up: `notes.example.com`, `http://192.168.1.20:8080`, and
`https://notes.tailnet-name.ts.net` can all be the same instance, and only the person who deployed it
knows which one this machine should use. The web app cannot solve this either — it lives on the far
side of the connection the CLI is trying to establish.

So the address comes from outside, once per machine, in this order:

1. `lewnotes login https://notes.example.com` — positional, preferred for scripts and docs
2. `--server https://…`
3. `LEWNOTES_SERVER`
4. the existing `server = "…"` in `config.toml`
5. a single prompt: `Server URL: ` (the *only* thing a human ever types in this flow)

It is validated against `GET /api/v1/health` before anything else happens, and `setup_required` is
reported instead of a confusing 401. After a successful login, `server` is written to config and is
also embedded in the `lnt.` token, so **no later command, and no agent, needs to supply it again.**
That is the actual promise: not zero configuration ever, but one address once, zero secrets always.

| Moment | Where the origin comes from |
|---|---|
| First `lewnotes login` | positional / flag / env / config / one prompt |
| Login handoff payload | encrypted by the server, verified against the origin the CLI connected to |
| Stored `lnt.` token | `base64url(origin)` in the token itself |
| Every later CLI command | config file, or the token when config is absent (§8) |
| Agent container / CI | `LEWNOTES_TOKEN` alone — the token carries the origin |

The web app's job is to hand the human the exact command, not to discover anything for the CLI:
`/settings/instance` shows a **Connect a machine** block with a copy button containing
`lewnotes login https://notes.example.com`, plus the `LEWNOTES_TOKEN` line for agents. The human still
types nothing secret into a terminal.

Explicitly deferred: mDNS/DNS-SD discovery (`_lewnotes._tcp`) for same-LAN homelab setups. It is
plausible and cheap on the server side, but it fails across VLANs, Tailscale, and remote access, needs
a discovery dependency in the CLI, and has spoofing considerations. Not in this slice; `--server`
remains the escape hatch if it ever lands.

### 4.1 Browser loopback — default on a desktop

1. CLI resolves the server URL (`--server` / `LEWNOTES_SERVER` / config). If none, one prompt, then it
   is remembered. It probes `GET /api/v1/health` and refuses if `setup_required`.
2. CLI binds `127.0.0.1:0` (random port), path `/cb`, and generates:
   - an X25519 keypair,
   - a 32-byte `state`,
   - a display name (hostname, or `--device-name`).
3. `POST /api/v1/cli/login` with `{public_key, state, device_name, client, redirect_uri}`.
4. CLI prints and opens `approve_url` (`/cli/authorize?id=…`).
5. The page shows instance, signed-in user, editable device name, requesting client
   (`lewnotes/0.1.0 (darwin/arm64)`), the server origin, and an expiry countdown. Authorize / Deny.
6. On approve the server mints the device token, builds the encrypted envelope, stores it, and returns
   `{redirect_url: "http://127.0.0.1:PORT/cb?id=…"}`. The page navigates there.
7. The CLI's loopback handler answers the browser with a small HTML page ("Authenticated — you can
   close this tab"), while the CLI's own poll loop calls `GET /api/v1/cli/login/{id}` with
   `X-Lewnotes-Handoff: <state>`, decrypts, verifies the origin and `GET /api/v1/auth/me`, then
   atomically writes config.
8. If the browser never returns, the CLI still completes through polling; the loopback server is only
   a confirmation page (some browsers also block a redirect that follows a POST).

The browser never carries the token or the envelope, and never carries `state` either — only the
request id. The CLI fetches the ciphertext directly over the same HTTPS connection it already trusts.

`redirect_uri` validation: `http` only, host must be `127.0.0.1` or `[::1]`, explicit port, path `/cb`.
Anything else is rejected at creation time; the CLI silently falls back to device code if binding fails.

### 4.2 Headless — SSH, containers, no browser (`--no-browser`, or no TTY to open one)

1. Same request, minus `redirect_uri`, plus `--no-browser`.
2. CLI prints:
   ```
   Open  https://notes.example.com/cli
   Code  7K4M-9QRT          (expires in 10:00)
   ```
3. The user opens the URL anywhere (phone, another laptop), signs in, enters the code at `/cli`, and
   approves the same authorize panel.
4. The CLI polls every 2s with jitter and backoff to 5s; on approval it fetches, decrypts, verifies,
   stores.

No copy/paste of a secret and no local listener. This is the RFC 8628 device-code shape, with the
payload encrypted to the CLI key so a poll response is useless to anyone else.

### 4.3 CI and agents — unchanged

`LEWNOTES_TOKEN` in the environment, or `lewnotes login --token-stdin --json`. Agents cannot click
Approve, so this stays the documented path in the CLI skill and in error messages. `lewnotes login`
with no TTY and no `--no-browser` fails fast with that instruction instead of waiting for a human.

---

## 5. The handoff format

Server, at approval time, for request `id = 01JC8…`:

```json
payload = {
  "v": 1,
  "server": "https://notes.example.com",
  "token": "lnt.aHR0cHM6Ly9ub3Rlcy5leGFtcGxlLmNvbQ.01JC8….kJ8s…",
  "token_id": "01JC8…",
  "device_name": "caleb-laptop",
  "username": "caleb",
  "instance_name": "home",
  "issued_at": "2026-11-25T18:04:12Z"
}
```

```
shared  = X25519(server_ephemeral_private, cli_public_key)       // 32 bytes
key     = HKDF-SHA256(ikm=shared, salt=request_id, info="lewnotes-cli-handoff-v1", 32)
nonce   = 12 random bytes
ct      = AES-256-GCM(key, nonce, plaintext=payload_json, aad=request_id || "\x00" || server_origin)

envelope = {"v":1,"epk":"<b64url server ephemeral public key>","nonce":"<b64url>","ct":"<b64url>"}
handoff  = "lnh1." + base64url(envelope_json)
```

- The server ephemeral keypair is generated **inside the approve handler** and its private half is
  discarded immediately, so no decryption key is ever persisted. The CLI's public key is stored on
  the request row.
- AAD binds the envelope to one request and one origin; a blob moved to another request fails to open.
- `lnh1.` is versioned separately from `lnt.` so the two can never be confused. Like `lnt.`, `.` never
  appears in base64url, so splitting is unambiguous.
- The CLI zeroizes the derived key and shared secret after decrypting; the ephemeral private key is discarded with the process.

CLI side, standard library only (`crypto/ecdh`, `crypto/hkdf`, `crypto/aes`, `crypto/cipher`; both
modules are on Go 1.25+, so no new dependency):

1. Parse `lnh1.`, check `v == 1`.
2. Recompute `shared` with its ephemeral private key; any failure is `ErrInvalidHandoff`.
3. Decode payload; reject if `server` origin ≠ the origin the login was started against.
4. Require a non-empty `token` that `client.ParseToken` accepts and whose embedded origin == `server`.
5. `GET /api/v1/auth/me` with that token; require `200` and matching `username`.
6. Save config atomically. On any failure, leave the previous credential untouched.

---

## 6. API surface (additions)

All errors use the existing problem+json shape.

```
POST /api/v1/cli/login
  unauth · rate-limited 10/min/IP
  → {"public_key","state","device_name","client","redirect_uri"?}
  ← 201 {"id","approve_url","user_code","expires_at","poll_after_ms":1000}

GET  /api/v1/cli/login/{id}
  header X-Lewnotes-Handoff: <state> · no session
  ← 200 {"status":"pending","expires_at"}
  ← 200 {"status":"approved","handoff":"lnh1.…"}   (handoff returned exactly once)
  ← 200 {"status":"denied"|"expired"|"cancelled"}
  ← 410 login_consumed
  ← 404 login_not_found for both an unknown id and a wrong state
  ← 429 poll rate-limited (60/min per request)

GET  /api/v1/cli/login/{id}/preview
  session required · for /cli/authorize
  ← 200 {"id","device_name","client","created_at","expires_at","server","status"}

POST /api/v1/cli/login/{id}/approve
  session + CSRF
  → {"device_name"?}   (editable on the page; falls back to the requested name)
  ← 200 {"status":"approved","redirect_url"?,"device_name"}

POST /api/v1/cli/login/{id}/deny
  session + CSRF
  ← 200 {"status":"denied"}

POST /api/v1/cli/login/resolve
  session + CSRF · rate-limited 10/min/session
  → {"user_code":"7K4M-9QRT"}
  ← 200 same shape as preview, or 404 invalid_login_code

DELETE /api/v1/cli/login/{id}
  header X-Lewnotes-Handoff: <state> · best-effort cancel on Ctrl-C
  ← 204
```

Details:

- `state` is stored as `sha256(state)`; the plaintext never lands in the DB or logs.
- `user_code` is 8 Crockford base32 chars, displayed `XXXX-XXXX`, unique among live requests, and
  compared case-insensitively after stripping dashes and folding I/L/O. Resolutions are rate-limited
  per session; a failed lookup does not reveal whether a code exists.
- `client` is `lewnotes/<version> (<goos>/<goarch>)`, length-capped at 100, and echoed on the token row
  as `user_agent` so `/settings/tokens` shows where it came from.
- Approving revokes nothing. Re-running `login` on the same machine mints a new token and leaves the
  old one in the list (the user can revoke it); the CLI prints the token name so it is findable.
- A sweeper deletes requests older than 24h at startup and hourly.
- OpenAPI document and its route-coverage test are updated in the same change.

### 6.1 Schema — migration `0004`

```sql
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE cli_login_requests (
  id           TEXT PRIMARY KEY,
  public_key   BLOB NOT NULL,
  state_hash   TEXT NOT NULL,
  device_name  TEXT NOT NULL,
  client       TEXT NOT NULL,
  redirect_uri TEXT NOT NULL DEFAULT '',
  server_origin TEXT NOT NULL,
  user_code    TEXT NOT NULL,
  status       TEXT NOT NULL,          -- pending|approved|denied|consumed|expired|cancelled
  user_id      TEXT,
  token_id     TEXT,
  handoff      TEXT,                   -- lnh1…, set on approve, nulled on consume
  attempts     INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  approved_at  INTEGER,
  consumed_at  INTEGER
);
CREATE UNIQUE INDEX cli_login_requests_code ON cli_login_requests(user_code) WHERE status = 'pending';
CREATE INDEX cli_login_requests_expiry ON cli_login_requests(expires_at);
```

TTL: `expires_at = created_at + 10m`. Status transitions are the only mutation path (no ad-hoc SQL).

---

## 7. Web app changes

| Route | Change |
|---|---|
| `/cli/authorize` | New standalone page (auth-only, no sidebar): instance, account, device name (editable), client, origin, countdown, Authorize / Deny, "wrong server?" hint when `server` ≠ current origin. Redirects to `/login?next=…` when signed out. |
| `/cli` | New page: code entry (`7K4M-9QRT`), resolves and forwards to the authorize panel. Reused by the headless flow. |
| `/settings/instance` | New owner page for public URL (§3). Also the **Connect a machine** block: the copyable `lewnotes login https://<public-url>` command and the `LEWNOTES_TOKEN` snippet for agents. |
| `/settings/tokens` | Description and empty state lead with `lewnotes login`; manual token creation remains, with the issued token shown once. |
| sidebar footer | Add "Instance" next to "Tokens". |

shadcn-svelte components already vendored cover all of this (`Field`, `Input`, `Button`, `Dialog`,
`Alert`, `Empty`). The code input uses an existing `Input` with a monospace class; do not add a new
component library for a segmented OTP field.

---

## 8. CLI changes

`cli/cmd/lewnotes/login.go` becomes a small state machine over `cli/client/handoff.go`:

- `login [server]` — optional positional URL so scripts can skip the prompt entirely.
- Flags: `--no-browser`, `--open` (force), `--device-name`, `--json`, plus the existing
  `--token-stdin`/`--token` compatibility paths.
- **Existing gap to close here:** `app.connect` currently errors with "no server configured" when only
  `LEWNOTES_TOKEN` is set, even though `lnt.` tokens already embed the origin. It must fall back to
  `client.ParseToken(token).ServerURL` after the config and before the error, with an explicit
  `--server`/`LEWNOTES_SERVER` still winning. Without this, "the token knows where the server is" is
  only true for `login --token-stdin`, not for agents, which is the case it matters most.
- Sequence: resolve origin → health/setup check → create request → loopback or device code → poll ≤ TTL
  → decrypt → verify → save → success line. Ctrl-C sends a best-effort `DELETE` and exits `1`.
- New `client` methods: `CreateLogin`, `GetLogin`, `ResolveLogin`, `PreviewLogin`, `CancelLogin`, and
  `ApproveLogin`/`DenyLogin` for tests; `client.NewLoginKey`, `NewLoginState`, and `LoginKey.Decrypt`
  implement the envelope.
- `--json` output on success: `{server, username, instance_name, device_name, token_name, auth}`.
  Never include the token or handoff.
- Error codes (in the JSON envelope, not new exit codes): `login_denied`, `login_expired`,
  `login_consumed`, `login_cancelled`, `handoff_invalid`. Exit `1`; unreachable stays `6`; a bad
  `--server` stays `2`.
- `--help --json` catalogue and the embedded `lewnotes-cli` skill are updated in the same change
  (the skill currently tells agents to paste a token; it should say `LEWNOTES_TOKEN` first, `login`
  second).

---

## 9. Tests

**Server unit (implemented)**
- Envelope round-trip; tampered ciphertext; wrong key; wrong AAD; wrong request id; wrong origin;
  bad scheme; non-X25519 public key. (`internal/handoff`)
- Request lifecycle: create → pending → approve → approved → consumed; second take is `ErrConsumed`;
  deny; expiry; user-code normalization and lookup; sweep. (`internal/clilogin`)
- `state` is stored hashed and compared in constant time; a wrong state is reported as not-found.
- `redirect_uri` validation covers scheme, loopback host, explicit port, `/cb`, and rejected
  query/fragment/userinfo (live-checked through creation; no table test yet).
- Public URL precedence (config > settings > derived) is implemented in `effectivePublicURL` and was
  live-checked; the `409 settings_from_config` branch has no unit test yet.

**CLI (implemented)**
- `LoginKey.Decrypt` covers a real server-shaped envelope, wrong request id, wrong origin, and
  malformed blobs.
- Device-code and loopback flows were exercised against a real server: create → resolve/approve →
  encrypted handoff → 0600 config → `whoami`; the loopback callback page was fetched and returned the
  confirmation HTML; denial exits `1`. There is no `httptest` login state-machine test yet.
- The help catalogue golden was regenerated for the new login flags.

**Web**
- `npm run check` clean and `npm run build` succeeds; a manual pass on `/cli`, `/cli/authorize`, and
  `/settings/instance` in a real browser is still pending (this environment has no browser).

**Manual acceptance (the real bar)**
1. Fresh config: `lewnotes login` against `http://localhost:5173` (public URL derived) → approve in the
   browser → `whoami`, `note list`, `search` all work.
2. Set public URL to a different origin in `/settings/instance`, restart, mint a token, confirm the
   token and login URL use it.
3. `--no-browser` over an SSH session: code → approve on the desktop → CLI completes.
4. Deny in the browser → CLI exits `1` quickly with `login_denied`.
5. `LEWNOTES_TOKEN` path still works with no config at all.

---

## 10. Done when

**Verified 2026-11-25 (live):** health validation returned version/instance/setup for the typed
origin; saving `public_url` made `/api/v1/settings` report `source: "settings"`; a headless CLI login
completed with a code, approval, encrypted handoff, a 0600 config, and a working `whoami`; the
loopback flow completed, served its confirmation page, and honored a device rename; denial exited `1`
with the denial message. Web pages compile and build; they have not been viewed in a browser.

- A machine with a browser needs exactly one command and zero typed secrets; the only possible prompt
  is the first-run server URL.
- A headless machine completes with a short code, no local listener, no paste.
- `/settings/instance` controls the origin baked into tokens and login links, and refuses to fight the
  config file.
- The paste/`--token-stdin`/`LEWNOTES_TOKEN` paths still work and are still documented.
- `go test ./...` (server and CLI), `go vet ./...`, and `npm run check` pass; the OpenAPI route-coverage
  test covers the new endpoints; the manual acceptance list above is exercised and recorded in a
  walkthrough.

---

## 11. Decisions (confirmed 2026-11-25)

1. **Ten-minute TTL** and 2s poll interval — long enough for a password manager, short enough for a
   single-use blob.
2. **No loopback `POST`** — we use a top-level redirect, which sidesteps mixed-content and Private
   Network Access preflight rules in every current browser. This means the callback URL carries the
   request id; the CLI fetches the ciphertext itself.
3. **Re-login mints a new token and leaves the old one** rather than rotating in place. Simpler, and
   revocation stays explicit.
4. **`settings` table, not config.toml writes.** If the operator wants the URL in version control,
   `server.public_url` still wins.
5. **No scopes.** The instance is single-owner today; adding scopes before there is a second role would
   be guesswork. The approval page has a place for them later.
6. **Encrypted envelope, not a JWT.** A JWT is signed, not confidential: base64url-encoding the
   server URL and API key into one would expose both to anyone who sees it. `lnh1.` gives the
   requested property — one opaque token that carries the origin and credential — while keeping the
   payload readable only by the CLI that asked. The device token inside is still an ordinary `lnt.`
   credential, so the same flow handles rotation: run `lewnotes login` again and revoke the old token.
7. **The owner types the address once, in the web app.** Validation pings that address's health
   endpoint before saving, so a typo is caught in the browser rather than on the first CLI login.

## 12. Out of scope for this slice

OS keyring storage, token rotation/refresh, per-token scopes, multiple users or roles, OAuth-style
third-party clients, and QR codes for the device-code page. Each is additive on this format.
