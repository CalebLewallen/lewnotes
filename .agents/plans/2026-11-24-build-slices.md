# Build plan: lewnotes in slices

**Status:** active · **Started:** 2026-11-24 · **Spec:** [`../../lewnotes-spec.md`](../../lewnotes-spec.md)

Goal of this plan: reach a testable vertical slice fast — **auth and notes in `lewnotes-core`, then the web UI on top of them** — before widening scope to todos, whiteboards, TUI, and release hardening.

## How to use this plan

- One slice per working session where possible. A slice is a coherent, shippable increment, not a checklist of files.
- A slice is **done** only when its "Done when" checks pass. Compiling is not done.
- At the start of a slice: read its entry, load the relevant skills, and confirm open decisions.
- At the end: mark the status table, append a walkthrough in `.agents/walkthroughs/`, and update the spec if a decision changed.
- If a slice needs more than two sessions, split it and record the split here rather than stretching it.

## Slice map

| Slice | Name | Depends on | Status |
|---|---|---|---|
| S0 | Scaffold, skills, and repo | — | ✅ done |
| S1 | Core plumbing: config, vault FS, SQLite, scan | S0 | ✅ done |
| S2 | Auth in core | S1 | ✅ done |
| S3 | Notes in core: CRUD, links, search, SSE | S2 | ✅ done |
| S4a | Web UI: login + read-only browsing | S3 | ✅ done |
| S4b | Web UI: editor, palette, conflicts, SPA embed | S4a | ⬜ |
| S5 | CLI + agent surface | S3 (S2 for login) | ⬜ |
| S6 | Attachments end to end | S3, S4 | ⬜ |
| S7 | Todos | S3 | ⬜ |
| S8 | Whiteboards | S3, S4 | ⬜ |
| S9 | TUI | S3, S5 | ⬜ |
| S10 | Docker, CI, release | S4+ | ⬜ |

Rationale for the order: auth has to exist before any UI can be tested realistically; notes are the narrowest full vertical (disk → index → HTTP → UI); S4 is the first point where the product is genuinely usable; everything after that is additive and can be reordered freely.

---

## S0 — Scaffold, skills, repo ✅

**Delivered**

- `go.work` with `server/` and `cli/` modules; stdlib-only `lewnotes-core` serving `GET /api/v1/health`, problem+json 404s, request logging, panic recovery, graceful shutdown, `-healthcheck` for containers.
- `cli/client` with a health call + tests; `lewnotes` binary with `version`, `help`, `--json`, and a JSON command catalogue seed.
- `web/` SvelteKit 3 / Svelte 5 minimal TS app, Tailwind 4 wired, static-adapter SPA config, dev proxy `/api → :8080`, status page that pings health.
- `docker/Dockerfile` (distroless, non-root, healthcheck) and `docker/docker-compose.yml`.
- `AGENTS.md`, `.agents/rules/*` (5 rules), `.agents/plans/` (this file), `.agents/skills/` (12 skills installed: Svelte 5/Kit, Go, shadcn).
- Git initialized; public GitHub repo.

**Verified:** `go build ./...`, `go vet ./...`, `go test ./...` (server, cli); `npm run check`, `npm run build` (web).

---

## S1 — Core plumbing: config, vault, SQLite, scan ✅

**Delivered 2026-11-24:** `config` (TOML + `LEWNOTES_*` + source-named validation errors), `vault` (path/symlink safety, atomic serialized writes, ignored walks), `markdown` (surgical frontmatter `id:` insertion/replacement, ULID validation, title/excerpt extraction), embedded `migrations/0001_init.sql` (notes, links, tags, meta, FTS5), `index` (upsert, tags, FTS, meta, scan status), and the scanner (incremental diffing, ID assignment, duplicate reassignment, stale removal), wired into `lewnotes-core` with a startup scan plus `GET/POST /api/v1/index/{status,rescan}`. Verified with unit tests and a live vault run including DB deletion and rescan.

**Goal:** a server that can own a vault directory, assign stable IDs, and index notes into SQLite — with no auth and no notes API yet.

**Tasks**

1. `internal/config`: TOML loading (`github.com/pelletier/go-toml/v2`) + `LEWNOTES_*` env overrides + validation that names the offending key and its source. Wire every key in `lewnotes.example.toml`.
2. `internal/vault`: path resolution (relative, cleaned, symlink-escape guard), `Read`/`WriteAtomic`/`Stat`/`List`/`Walk`, ignore globs, per-path mutex. Table tests for traversal (`../`, absolute, symlink out, reserved `.lewnotes/`).
3. `internal/markdown`: frontmatter read + **surgical `id:` insertion**; frontmatter parse via `gopkg.in/yaml.v3` into a generic map; title derivation. Golden tests: no frontmatter, existing block, CRLF, malformed block, duplicate `id`.
4. `internal/index`: `modernc.org/sqlite`, DSN pragmas, `database/sql` with one writable connection, migration runner on `PRAGMA user_version`, `migrations/0001_init.sql` (notes, links, tags, meta, FTS5).
5. `internal/index/scan.go`: walk, ignore, hash, upsert; assign missing IDs during the scan; progress state in `meta`; full vs incremental scan.
6. Routes: `GET /api/v1/index/status`, `POST /api/v1/index/rescan` (still unauthenticated in this slice — accept and note it).

**Done when**

- `cd server && go test ./... && go vet ./...` passes, including path-safety and frontmatter goldens.
- Point the server at a fixture vault: IDs are written to every `.md`, `GET /api/v1/index/status` reports counts, `POST /index/rescan` is idempotent.
- Delete the DB, restart, rescan: note IDs and paths are identical.

**Notes/risks**

- Frontmatter insertion is the highest-risk code in the slice; write it character-first with tests before wiring the scanner.
- Do not store absolute paths in SQLite. Store vault-relative POSIX paths.

---

## S2 — Auth in core ✅

**Delivered 2026-11-24:** `0002_auth.sql` (users, sessions, api_tokens), `internal/auth` (argon2id PHC hashing, `lnt.` token encode/decode, sessions with hashed IDs, device tokens, in-memory rate limiting), default-deny middleware over `/api/v1/*` with CSRF for cookie mutations, and `/auth/{status,setup,login,logout,me,password}` plus `/tokens` CRUD. Verified with race-enabled tests and `server/testdata/scripts/auth.sh` against a live server, including a log scan for leaked secrets.

**Goal:** only a password-holding owner can read or write anything; device tokens exist and are safe.

**Tasks**

1. `migrations/0002_auth.sql`: users, sessions, api_tokens.
2. `internal/auth`: argon2id hashing (`golang.org/x/crypto`), constant-time verify, password policy.
3. Sessions: 32-byte ID, `HttpOnly; Secure; SameSite=Lax` cookie, sliding 30-day expiry, store user agent + IP, revoke on logout/password change.
4. CSRF: double-submit token for cookie-authenticated state-changing requests; reject mismatches with `403`.
5. Middleware: require auth on every `/api/v1/*` except the documented allowlist; attach principal to context.
6. Device tokens: `lnt.<base64url(server_url)>.<ulid>.<secret>` encode/decode in `internal/auth/token.go`; store `sha256(secret)` + display prefix; list/revoke endpoints; `last_used_at` throttled updates.
7. Rate limiting: in-memory per-IP limiter for login (5/min, backoff), token verification (30/min), general (600/min/token).
8. Routes: `/auth/status`, `/auth/setup`, `/auth/login`, `/auth/logout`, `/auth/me`, `/auth/password`, `/tokens` (GET/POST/DELETE).
9. **Decide** token scopes (spec §18.1). Recommended v1: single full-access scope, field reserved in schema, documented.

**Done when**

- `go test -race ./...` passes, covering: setup race (two concurrent setups → one user), login/logout, CSRF rejection, token round-trip + tampered token, revoked token rejected, rate-limit trip, and "no secret in log output".
- `curl` script (`server/testdata/scripts/auth.sh`) walks setup → login → cookie-authed `index/status` → token creation → bearer-authed call.

**Notes/risks**

- Token format embeds the URL; keep encode/decode in one tested package so CLI and server cannot drift.
- Cookie `Secure` must be configurable for localhost dev; document it.

---

## S3 — Notes in core: CRUD, links, search, SSE ✅

**Delivered 2026-11-24:** `0003_links.sql` (line numbers, ambiguity candidates, reverse indexes), `internal/links` (parse/alias/fragment/embed, exact → unique suffix → unresolved/ambiguous), `internal/notes` (create/read/list/update/patch/move/delete, ETags and `If-Match`, trash, tree, raw files, rewrite-on-rename), index queries and link indexing in the scanner, `internal/search` grammar + FTS5 with title boost and snippets, `internal/events` ring buffer/replay/resync, all note/tree/file/search/events/meta routes, and a hand-written OpenAPI document whose route coverage is tested. Verified with unit/API tests, `go test -race ./...`, and a live `notes.sh` smoke. Web UI consumes this API in S4a.

**Goal:** the full notes API the web UI will consume, tested without a browser.

**Tasks**

1. `internal/notes`: create/read/update/patch/move/delete(soft), ETag = `sha256(content)`, `If-Match` enforcement (`428`/`412`), atomic writes through `vault`.
2. Routes: `GET/POST /notes`, `GET /notes/{id}`, `GET /notes/by-path`, `PUT/PATCH/DELETE /notes/{id}`, `POST /notes/{id}/move`, `GET /notes/{id}/links|backlinks`, `GET /tree`, `GET /files/{path}`.
3. `internal/links`: parser (wikilinks, aliases, fragments, embeds), resolver (exact → shortest unique suffix → unresolved/ambiguous), reverse index on write, rewrite-on-rename.
4. `internal/search`: query grammar → FTS5 (`bm25`, title boost), typed results (note/todo/attachment), snippets with highlight spans.
5. `internal/events`: SSE hub, ring buffer (1000), `Last-Event-ID` replay, heartbeat, `origin` from `X-Lewnotes-Client`; emit on every mutation.
6. API contract: **decision:** hand-written `server/internal/api/openapi.json`, embedded and served at `/api/v1/openapi.json`. A Go test compares it against the route table, so a new route cannot ship undocumented. Recorded in spec §10.1.
7. Regenerate `cli/client` types from the spec (deferred to S5); the S4a web client is a small typed `fetch` wrapper.

**Done when**

- Tests: link resolution table (case, suffixes, ambiguity, ghosts), rename rewrites inbound links and reports counts, stale `If-Match` → `412` with no file change, search grammar operators, SSE replay from `Last-Event-ID`.
- Rebuild test: delete DB → rescan → `GET /notes` and search results are identical.
- `openapi.json` validates and covers every route.

**Notes/risks**

- Move+rewrite is multi-file: define partial-failure semantics now (move wins, warnings returned) and test it.
- Keep the mutation path single-entry so events and index updates cannot be forgotten.

---

## S4 — Web UI on the notes API (first usable build)

**Goal:** sign in from a browser, browse/edit/search notes with links and backlinks, and handle conflicts. This is where manual testing starts.

**S4a delivered 2026-11-24 (steering stop):** `DESIGN.md`; hand-built primitives on the design tokens instead of the shadcn CLI; typed API wrapper with credentials, CSRF, and problem+json errors; `/setup`, `/login`, guarded app shell with sidebar tree/filter and mobile drawer; read-only `/n/<path>` with sanitized markdown, API-resolved wikilinks, unresolved-link list, and backlinks; recent-note list; logout. Verified `npm run check`, `npm run build`, and a live dev-proxy/API smoke. Tasks 1, 3, 4, 10 and the read half of task 5 are done; the rest is S4b.

**Remaining S4b:** editor + autosave/ETag, backlinks actions/ghost create, rename dialog, ⌘K search, conflict diff, SSE client refresh, generated clients, and `go:embed` of the built SPA.

**Tasks**

1. **Write `DESIGN.md` first** (spec §12.4): tokens, type scale, spacing, dark theme, component inventory, keyboard map, wireframes, mobile breakpoint.
2. shadcn-svelte init: `components.json`, `src/lib/components/ui`, theme variables mapped to the DESIGN.md tokens, `utils.ts`.
3. API layer: generated types from `openapi.json`, one `fetch` wrapper (credentials + CSRF header + problem+json parsing), typed errors for 401/412.
4. Auth screens: `/setup`, `/login`; route guard that checks `/auth/status` + `/auth/me`; logout.
5. Notes: sidebar tree (`/tree`), note route `/n/<path>`, textarea editor + preview (remark/rehype + wikilink/embed plugin), autosave with ETag, explicit save, unsaved indicator.
6. Backlinks panel, outgoing links, ghost-link create, rename dialog (with link-rewrite count toast).
7. ⌘K search palette with typed results and highlighting; result navigation.
8. Conflict UX: `412` → diff panel (reload / overwrite), never silent merge.
9. SSE subscription: refresh tree/search, reload open note when clean, banner when dirty.
10. Mobile-tolerant layout: drawer sidebar, full-width editor, capture button.

**Done when**

- Manual script passes against a real server: setup → login → create note → edit → add `[[link]]` → create ghost → see backlink → rename note → link updated → two-tab conflict shows diff → search finds body text → refresh keeps session.
- `npm run check && npm run build` passes; built SPA is served by `lewnotes-core` from `/` (embed the static build with `go:embed` in this slice).
- Works at 390px width without horizontal scrolling.

**Notes/risks**

- Do not build a markdown editor framework here: textarea + preview ships the slice. CodeMirror 6 (wikilink completion, syntax) is a follow-up, not a blocker.
- If shadcn-svelte's CLI fights SvelteKit 3, hand-vendor the primitives needed (button, dialog, input, command, toast) and note it.

---

## S5 — CLI + agent surface

**Goal:** a human and an LLM can do everything S3/S4 can from a terminal.

**Tasks**

1. `internal/config` for CLI: `~/.config/lewnotes/config.toml` (0600), `LEWNOTES_SERVER`/`LEWNOTES_TOKEN` overrides, `--keyring` optional.
2. `lewnotes login`: interactive walkthrough exactly as spec §11.4 (URL probe → browser instructions → masked paste → decode + verify → store), plus `--server … --token-stdin --json` non-interactive path.
3. Commands: `logout`, `whoami`, `config get|set`, `note list|show|new|append|edit|rm|mv|links|backlinks`, `search`, `index status|rescan`.
4. Ref resolution (id → path → unique suffix → unique title; ambiguity exits 3) and `$EDITOR` round-trip with ETag.
5. `--json` envelope everywhere (stable `{ok, data, warnings, error}`), `--json=lines` for lists, documented exit codes.
6. `--help --json` complete: per-command args/flags/types/defaults/required, output keys, examples, read-only vs mutating.
7. Ship the `lewnotes-cli` skill; `lewnotes skills print|install --dir .agents/skills`.
8. Shell completion for bash/zsh/fish.

**Done when**

- Golden tests for JSON output of every read-only command.
- Non-interactive login works against `httptest` and stores/reads credentials correctly.
- A shell-script "agent simulation" completes: login (env token) → search → append to note → verify with `note show --json` → conflict path returns exit 5.
- `lewnotes --help --json | jq` fully describes the CLI with no undocumented commands.

---

## S6 — Attachments end to end

**Goal:** paste/drop an image in the browser or upload from the CLI and get a working embed.

**Tasks:** multipart upload endpoint with limits + MIME allowlist + collision handling; `GET /files/{path}` serving with ETag/caching; attachment index + orphan query; CLI `attach`; web editor paste/drop handler inserting the returned markdown; size/type error UI.

**Done when:** upload via curl and via browser both produce a note embed that renders; oversized file → `413` with a useful message; deleting a note leaves its attachment (and the orphan report lists it).

---

## S7 — Todos

**Goal:** `todo.txt` in designated files, readable and mutable from API, web, and CLI — without corrupting lines.

**Tasks:** `internal/todos` parser/serializer with token spans and golden corpus; todo tables + migration; `GET/POST/PATCH /todos`, projects/contexts; `todo:` search filters; CLI `todo *`; web `/todos` views (project/context/priority/due, inline complete); line-hash conflict handling.

**Done when:** todo.txt community-spec examples round-trip byte-identically; toggling done changes only prefix/priority; stale line hash → `409` with candidates; web and CLI both mutate the same line and the other sees it via SSE.

---

## S8 — Whiteboards

**Goal:** `.excalidraw` files stored, rendered inline, and editable.

**Tasks:** `internal/boards` validation + storage; `GET/POST/PUT /whiteboards/{id}` with ETag; index element counts; web inline read-only render (lazy-loaded `@excalidraw/excalidraw`) + `/w/<path>` editor with debounced save; `![[file.excalidraw]]` embed in preview; CLI `board list|new|show`.

**Done when:** a fixture scene loads, edits persist, reopening shows changes, and a note embedding the drawing renders a preview.

---

## S9 — TUI

**Goal:** full reader/editor in the terminal over the same client.

**Tasks:** Bubble Tea app (`lewnotes tui`), sidebar tree + fuzzy finder, editor pane with `If-Match` saves, glamour preview toggle, backlinks panel, todo pane, search, conflict modal, keymap config (`default`/`vim`), theme.

**Done when:** create/edit/search/rename/conflict all work over SSH; no API-only capability needed for the flows; config file documented.

---

## S10 — Docker, CI, release

**Goal:** one-command deploy and a reproducible release.

**Tasks:** extend Dockerfile to build and embed the web SPA (multi-stage node → Go → distroless); compose with vault volume + healthcheck; GitHub Actions (Go test/vet/lint, web check/build, OpenAPI drift check, image build); version injection via ldflags/tags; backup/restore docs; security headers/CSP on the SPA; rate-limit and proxy docs.

**Done when:** `docker compose up` on a clean machine → setup → notes → CLI login works over the mapped port; CI is green on a fresh clone; `latest` + `vX.Y.Z` images publish multi-arch.

---

## Testing strategy (all slices)

| Layer | Tool | Rule |
|---|---|---|
| Pure logic (formats, links, grammar) | Go table tests + golden files | No I/O; corpus of real-world odd inputs |
| Storage/index | Go tests over `t.TempDir()` vaults + SQLite | Always include the delete-DB-and-rescan equivalence test |
| HTTP | `httptest` + real router | Auth, status codes, problem+json, ETags |
| Web | `svelte-check` + targeted component tests | Manual script per slice; Playwright only once S4 is stable |
| CLI | Golden JSON + `httptest` server | Exit codes are part of the contract |

## Risk register

| Risk | Where | Mitigation |
|---|---|---|
| Frontmatter/todo.txt corruption | S1, S7 | Surgical edits + byte-identical round-trip goldens before wiring |
| Link rewrite data loss on rename | S3 | Single mutation entry point, warnings surfaced, test partial failure |
| SvelteKit 3 / shadcn-svelte mismatch | S4 | Vendor the few primitives needed; don't block on CLI generators |
| OpenAPI drift | S3+ | Generate from handlers or fail CI on stale spec |
| Auth bypass on a new route | S2+ | Middleware default-deny + a test that enumerates routes |
| Token leakage via logs | S2, S5 | Rule + test asserting secrets never appear in log output |
| Scope creep into todos/whiteboards before S4 | all | Slice map is the gate; new ideas go to the spec's open questions |

## Next action

Continue **S4b** after steering: editor with explicit save/autosave and `If-Match`, command-palette search, rename + ghost-link flows, 412 conflict diff, SSE refresh, then embed the built SPA in `lewnotes-core`. The S4a read-only UI is the point to review interaction and visual direction before that work.
