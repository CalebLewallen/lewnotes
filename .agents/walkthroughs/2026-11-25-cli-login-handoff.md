# Walkthrough — S5b: frictionless CLI login handoff

**Date:** 2026-11-25 · **Plan:** [S5b spec](../plans/2026-11-25-cli-login-handoff.md) · **Status:** code complete; live CLI check passed; browser pages not viewed

## What shipped

- **Instance address in the web app.** `/settings/instance` shows the effective public URL, its
  source (`config`, saved setting, or derived from the request), `server.listen`, and `trust_proxy`.
  The owner can type the address or accept the current origin. **Test** performs a real
  `GET <url>/api/v1/health` (5s timeout, no redirects) and reports version, instance name, and setup
  state before **Test and save** persists it. If `server.public_url` is set in config, the field is
  read-only and `PATCH` returns `409 settings_from_config`.
- **Health validation endpoint.** `POST /api/v1/settings/validate` is session + CSRF protected and
  rate-limited per owner. It also confirms the candidate is a lewnotes server rather than any host
  that answers 200.
- **One encrypted credential.** The CLI generates a per-login X25519 keypair. On approval the server
  mints an ordinary `lnt.` device token, packs `{server, token, token_id, device_name, username}` into
  a JSON payload, and encrypts it with HKDF-SHA256 → AES-256-GCM. AAD binds the ciphertext to the
  request id and origin. The result is `lnh1.<base64url(envelope)>`, returned exactly once.
- **Browser loopback flow (default).** CLI binds `127.0.0.1:0`, registers a request with
  `redirect_uri=http://127.0.0.1:<port>/cb`, and opens the approval page. The page approves, navigates
  to `/cb?id=…`, and the CLI's poll loop fetches the handoff with its `state` header. The browser never
  sees the token, the envelope, or the state.
- **Headless flow (`--no-browser`).** Prints `Open https://…/cli?code=XXXX-XXXX` and polls. The code
  is 8 Crockford base32 chars, valid for 10 minutes, normalized for case/dashes and I/L/O confusion.
- **Web approval pages.** `/cli` accepts the code; `/cli/authorize` shows instance, requesting client,
  expiry countdown, and an editable device name, with Authorize/Deny and a warning when the CLI's
  origin differs from the origin in the browser. Both redirect to `/login?next=…` when signed out.
- **`LEWNOTES_TOKEN` alone works.** `app.connect` now falls back to the origin embedded in the token
  when no server is configured, so agents and CI need one variable.
- **Rotation is the same flow.** Re-running `lewnotes login` mints a new token (named after the device)
  and leaves the old one listed for explicit revocation.

## Verification

Automated:

- `cd server && go test ./...`, `go vet ./...` — pass, including `internal/handoff` round-trip/tamper
  tests, `internal/clilogin` lifecycle/expiry/code tests, and the OpenAPI route-coverage test for the
  ten new routes.
- `cd cli && go test ./...`, `go vet ./...` — pass, including the CLI-side envelope test; the help
  golden was regenerated for the new login flags.
- `cd web && npm run check` — 0 errors, 0 warnings. `npm run build` — succeeds.
- `bash scripts/build-web.sh` — succeeds (embedded SPA).

Live (temporary server on `127.0.0.1:8099`, temporary vault and data dir):

1. Setup → session + CSRF cookies; `POST /settings/validate` returned
   `{valid:true, version:"dev", instance_name:"live", setup_required:false, latency_ms:...}`.
2. `PATCH /settings` saved `http://127.0.0.1:8099`; `GET /settings` then reported
   `source:"settings"`.
3. Headless: `lewnotes login --no-browser --server …` printed a code; resolving and approving it
   completed the CLI, wrote a `0600` config, and `whoami --json` reported the approved token name and
   instance.
4. Loopback under a PTY: the CLI registered `redirect_uri`, the approval response returned
   `http://127.0.0.1:<port>/cb?id=…`, fetching it returned the confirmation HTML, and the CLI completed.
   Renaming the device on approval propagated into the stored `device_name` and token name.
5. Denial: the CLI printed the denial message and exited `1` with no config written.

Not verified:

- The three new pages (`/settings/instance`, `/cli`, `/cli/authorize`) have not been rendered in a
  browser; this environment has none. Layout, focus order, and the countdown are unviewed.
- No `httptest` login state-machine test yet; flows were exercised against a real server.
- The `409 settings_from_config` branch is implemented but not unit-tested.

## Follow-ups

- Browser pass on the three pages, including mobile width and the signed-out redirect.
- Add an API-level `httptest` for create → approve → poll → consumed, including wrong-state `404` and
  replay `410`.
- Consider `--keyring` and QR codes for the device-code page (out of scope here).
