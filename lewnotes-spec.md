# lewnotes — Product & Technical Specification

**Status:** Draft v0.1 (pre-implementation)
**Date:** 2026-11-24
**Author:** Spec assembled from a five-round design interview
**Related deliverables:** `DESIGN.md` (web UI design system), `AGENTS.md` (agent-facing repo guide)

---

## 1. Summary

lewnotes is a self-hostable, server-based notes application for a **single tenant**, built around a **plain-file vault**: markdown notes, `todo.txt`-syntax tasks, and Excalidraw whiteboards, all stored on the server's disk and served by a Go + SQLite core.

The defining trait is that **lewnotes-core is data-first**: the server is the product, and the web UI, TUI, and CLI are three peer interfaces over the same HTTP API. There is no sync protocol and no local vault — every client reads and writes the server's files through the API.

Short-term name for the request: **lewnotes**.

---

## 2. Goals

1. **Self-hostable, single-tenant server.** One instance = one owner = one vault. Reachable from the open internet, so authentication is mandatory on every interface and every route.
2. **Files are the source of truth.** Notes are `.md`, tasks follow `todo.txt` syntax, drawings are `.excalidraw` JSON, attachments are ordinary files. The vault remains useful with `grep`, `vim`, `git`, and Obsidian.
3. **Data-first architecture.** Every capability of the UI has a documented API endpoint. The CLI is a thin, scriptable projection of that API. No feature exists only in the Svelte app.
4. **Uniform auth semantics.** A single owner account, one credential model, and one login story across web, CLI, and TUI — with an explicit answer for "how does the CLI know where the server is?"
5. **Go server, SQLite storage, Docker deployment.** One container, one mounted vault volume.
6. **Agent-friendly.** A `--json` output mode on the CLI, a machine-readable command catalogue (`--help --json`), and a shipped skill so LLM agents can operate the vault safely.
7. **Repo conventions that scale.** All agent artifacts live under `.agents/` (skills, rules, plans, walkthroughs), with `AGENTS.md` at the root as the entry point.

## 3. Non-goals (v1)

- Multiple user accounts, sharing, per-note permissions, teams.
- A sync protocol or offline-first local vault on the client. Clients require the server.
- Native mobile apps (the web UI should be *tolerant* of a phone browser, nothing more).
- End-to-end encryption, note-level encryption, or a plugin system.
- Real-time multi-cursor collaborative editing (single owner means this is not the bottleneck).
- Graph visualization, daily-note/template automation, git auto-commit history — all candidates for later, not v1.
- Import/ingest tooling for existing Obsidian vaults (see §18).

---

## 4. Locked decisions

These came out of the interview and are treated as settled unless explicitly reopened.

| # | Area | Decision |
|---|------|----------|
| 1 | SQLite role | **Index + app state.** Files hold all content; SQLite holds a rebuildable index (FTS5, links, tags, todos) *plus* server-only state (sessions, tokens, rescan cursors, prefs). |
| 2 | Vault model | **Server owns one vault directory.** No local sync; every interface mutates the server's files. One tenant per instance. |
| 3 | External writes | **None.** lewnotes is the only writer; manual edits to vault files are unsupported behavior. |
| 4 | Deployment | **Docker image** is the primary artifact. Vault mounted as a volume. |
| 5 | Auth method | **Built-in single password** (argon2id), first-run setup screen, no external IdP. Auth lives behind one interface so OIDC could be added later. |
| 6 | CLI/TUI login | **Browser-issued long-lived token, pasted into the CLI**, where the token encodes the server URL so the client needs no pre-known address. |
| 7 | Token lifetime | **Long-lived device tokens**, revocable from a device/token list in the web UI. No refresh-token machinery. |
| 8 | LLM access | **CLI + skills.** `--json` everywhere, `--help --json` command catalogue, shipped skill file. Same auth flow as humans. |
| 9 | Todo storage | **Only designated files** (configurable globs), not any markdown file. |
| 10 | Whiteboards | **Separate `.excalidraw` files**, linked with `![[drawing.excalidraw]]`, rendered read-only inline, editable in a dedicated view. |
| 11 | Note identity | **Stable opaque ID + mutable path.** |
| 12 | Link syntax | **Obsidian-style wikilinks**, case-insensitive resolution, ghost (unresolved) links are a first-class state. |
| 13 | Rename behavior | **Rewrite all inbound wikilinks** in the same operation. |
| 14 | Core API | **REST + OpenAPI + SSE** change events. |
| 15 | Write conflicts | **Optimistic concurrency** via ETag/`If-Match`, `409`/`412` on stale writes. |
| 16 | ID storage | **YAML frontmatter `id:`** — IDs survive a full index rebuild and are visible in any editor. |
| 17 | Vault layout | **Minimal + configurable**: `inbox/` is the root for notes and new notes are always created inside it, in any subfolder structure; `attachments/` is upload-only and never accepts notes; no enforced tree inside `inbox/`. |
| 18 | MVP scope | **Notes end to end** (scan, CRUD, links, search, auth, API, web UI, CLI). Todos, whiteboards, TUI follow. |
| 19 | TUI scope | **Full reader/editor** — real markdown editing and navigation, not only capture. |
| 20 | Agent docs | **`AGENTS.md` at root + `.agents/{skills,rules,plans,walkthroughs}/`.** |
| 21 | UI stack | **Svelte 5 + shadcn-svelte + Tailwind 4**, dark-first, keyboard-centric, mobile-tolerant. |
| 22 | Todo file rule | **Configurable globs** (default `todo.md`, `todos/**/*.md`). |
| 23 | Todo line form | **Bare todo.txt lines, no checkbox bullets**, written byte-faithfully. |
| 24 | Repo layout | **Monorepo, multiple Go modules** (`server/`, `cli/`, `web/`) with a root `go.work`. |
| 25 | Search | **FTS5 + a shared query grammar** (`tag:`, `path:`, `has:`, `todo:`, negation) used by all clients. |
| 26 | Config | **TOML file + `LEWNOTES_*` env overrides** (Docker-first). |
| 27 | Attachments | **Upload endpoint** for any client; the web editor wires paste/drop to it and inserts the embed automatically. |

---

## 5. Vocabulary

| Term | Meaning |
|------|---------|
| **Vault** | The single root directory on the server holding all user files. |
| **Note** | A `.md` file in the vault, addressed by stable ID, located by path. |
| **Todo file** | A markdown file matching a configured glob whose non-blank lines are parsed as `todo.txt`. |
| **Todo** | One `todo.txt` line, addressed as `(file, line_number)` at mutation time. |
| **Whiteboard** | A `.excalidraw` JSON file. |
| **Attachment** | Any non-note, non-whiteboard file (images, PDFs). |
| **Ghost link** | A wikilink whose target does not exist yet; creatable in one click. |
| **Device token** | A long-lived credential issued from the web UI, encoding the server URL. |
| **Session** | A cookie-backed web login, server-side revocable. |

---

## 6. Repository layout

```
lewnotes/
├── go.work                      # workspace: ./server ./cli
├── AGENTS.md                    # agent entry point (points into .agents/)
├── DESIGN.md                     # web UI design system (deliverable of M0)
├── README.md
├── lewnotes.example.toml
├── docker/
│   ├── Dockerfile               # multi-stage: node → web dist; go → binary
│   └── docker-compose.yml
├── server/                      # module github.com/<owner>/lewnotes/server
│   ├── cmd/lewnotes-core/
│   │   └── main.go
│   ├── internal/
│   │   ├── config/              # TOML + env loading, validation
│   │   ├── vault/               # path safety, atomic writes, scanning, frontmatter
│   │   ├── index/               # sqlite schema, migrations, queries, FTS5
│   │   ├── notes/               # CRUD, moves, link rewriting
│   │   ├── links/               # wikilink parse/resolve/rewrite
│   │   ├── todos/               # todo.txt parse/serialize/query
│   │   ├── boards/              # excalidraw storage + validation
│   │   ├── search/              # query grammar → FTS5
│   │   ├── auth/                # password, sessions, device tokens
│   │   ├── api/                 # handlers, middleware, problem+json
│   │   ├── events/              # SSE hub + ring buffer
│   │   └── webui/               # go:embed of built SPA
│   ├── migrations/              # 0001_init.sql, ...
│   └── openapi/openapi.yaml     # generated, checked in
├── cli/                         # module github.com/<owner>/lewnotes/cli
│   ├── cmd/lewnotes/            # CLI + TUI entrypoint (one binary)
│   ├── client/                  # typed API client (importable, no server deps)
│   ├── commands/                # one file per command group
│   ├── tui/                     # Bubble Tea app
│   ├── internal/config/         # ~/.config/lewnotes/config.toml
│   └── skills/lewnotes-cli/SKILL.md   # shipped, installable skill
├── web/                         # SvelteKit 5 (adapter-static, SPA)
│   ├── src/routes/...
│   ├── src/lib/components/ui/   # shadcn-svelte
│   └── src/lib/api/             # generated client from OpenAPI
├── docs/
└── .agents/
    ├── skills/<name>/SKILL.md
    ├── rules/*.md
    ├── plans/YYYY-MM-DD-<slug>.md
    └── walkthroughs/YYYY-MM-DD-<slug>.md
```

**Module boundaries.** `cli/client` must not import server internals; it depends only on generated types and `net/http`. The web app consumes the same OpenAPI document. This keeps "data-first" honest: the contract is a file, not a convention.

**Binary count.** `lewnotes-core` (server) and `lewnotes` (CLI and TUI, TUI launched via `lewnotes tui`).

---

## 7. Vault model & file safety

### 7.1 Root and layout

- Configured path, e.g. `/data/vault` (`LEWNOTES_VAULT`). Created on first start with mode `0750`.
- On first run, scaffold only: `inbox/` and `attachments/`. `inbox/` is the root for notes: new notes are always created inside it, and subfolders there are free-form. The vault root is not a note destination. `attachments/` is system-managed for uploaded files: notes cannot be created or moved into it.
- Config names the folders so they remain configurable:

```toml
[vault]
path = "/data/vault"
inbox = "inbox"
attachments = "attachments"

[todos]
globs = ["todo.md", "todos/**/*.md"]

[attachments]
max_size_mb = 50
name_strategy = "original"   # original | hashed
```

- Ignored on scan: dot-directories (`.obsidian/`, `.git/`, `.lewnotes/`), `node_modules/`, `*.tmp`, and any path matching `vault.ignore` globs.

### 7.2 Path safety

- All paths are **relative to the vault root**, POSIX-separated, `.` and `..` rejected after cleaning.
- Resolution must be symlink-safe: `filepath.EvalSymlinks` result must remain under the resolved root. Symlinks pointing outside are ignored in scans and refused in reads/writes.
- Reserved names: nothing may be written into `.lewnotes/` (server-owned, see §7.5).

### 7.3 The server is the only writer

Per decision #3, no filesystem watcher reconciliation is required. Consequences:

- An in-process per-path mutex serializes writes to the same file.
- All writes are atomic: write temp file in the same directory → `fsync` → `rename` → `fsync(dir)`.
- Optional safety net (`index.reconcile_interval`, default `0` = off): a periodic rescan that only logs drift (files whose hash differs from the index) rather than merging content. Document that hand-editing while the server runs is undefined and may be overwritten.

### 7.4 Markdown notes and frontmatter

A note is any `*.md` file. lewnotes injects exactly one field:

```markdown
---
id: 01JC8Z9K2M4N6P8Q0R2S4T6V8W
title: Kitchen renovation   # user-owned, untouched if present
tags: [home, diy]           # user-owned
---

Body starts here. Links to [[plumbing-plan]] and ![[diagram.excalidraw]].
```

Rules:

- `id` is a ULID (26 chars, Crockford base32, lexicographically sortable).
- If a note has no frontmatter, lewnotes creates a minimal block containing only `id`.
- If frontmatter exists, `id:` is appended as the **last** line of the block to minimize diff noise; existing keys, order, quoting, and comments are preserved verbatim.
- If a note's `id` is invalid or duplicated, the server assigns a new ULID (writing it) and records a warning; the conflict is surfaced via `GET /api/v1/index/status`.
- Unknown frontmatter is never rewritten; it is parsed into a generic map and exposed in the API (`frontmatter`), and only used for search indexing of `title`/`tags`.
- Title for display: frontmatter `title`, else first ATX H1, else filename without extension.

### 7.5 Reserved directory (for later)

`.lewnotes/` is reserved for server-owned metadata (e.g. trash, cached exports). v0.1 writes nothing there except, optionally, a `trash/` on deletes if soft-delete is enabled (§18).

### 7.6 Deletes

Default: **soft delete** to `.lewnotes/trash/<timestamp>-<name>` with an API to restore or purge. Rationale: a browser misclick should not destroy a file on a server-owned vault. (Flagged in §18 as confirmable.)

---

## 8. Content formats

### 8.1 `todo.txt` tasks (decision #22/#23)

Only files matching `todos.globs` are parsed. Lines are **bare todo.txt**, no bullets:

```
(A) 2026-01-05 Fix the kitchen sink +home @errands due:2026-01-12
x 2026-01-07 2026-01-05 Order replacement cartridge +home
2026-01-06 Read up on P-traps +home +reading @computer
```

Parsing follows the community spec:

- Optional completion: leading `x ` followed by a `YYYY-MM-DD` completion date.
- Optional priority: `(A)`–`(Z)` at the start of an open task (uppercase only). Completion strips priority.
- Optional creation date `YYYY-MM-DD` directly after priority (or first, if no priority).
- Description: free text.
- `+project` and `@context` tokens: any non-whitespace run after a `+`/`@`, may repeat.
- `key:value` metadata: `due:2026-01-12`, `rec:1w`, `t:2026-01-05`, arbitrary keys preserved verbatim.

**Byte fidelity is a hard requirement.** The parser keeps the original line, token spans, and unknown metadata. Mutations are: toggle completion, change priority, change due date, append/remove project or context, edit description. The serializer applies changes *in place* and preserves everything else, including unusual spacing, trailing whitespace, and unknown key:value pairs. Golden round-trip tests are mandatory (`parse → serialize` byte-identical for untouched lines).

**Addressing.** Todos get no IDs in the file (that would corrupt the format). A todo is addressed by `(path, line)`, and every mutation carries the file's ETag plus the line's hash. If the line moved or changed, the server returns `409` with the current candidates. The index stores line hashes for this purpose.

**Completion semantics.** Marking done prepends `x <today> ` (and removes priority). Un-completing removes the marker and completion date. Config option `todos.archive_completed` (default `false`) could move completed lines to `done.md`, but v1 leaves them in place.

**Ordering.** lewnotes never reorders lines. Sorting/filtering happens at read time in queries.

### 8.2 Excalidraw whiteboards (decision #10)

- Stored as `*.excalidraw` files (UTF-8 JSON: `{type:"excalidraw", version, source, elements, appState, files}`), anywhere in the vault, conventionally `whiteboards/` (not enforced).
- The API treats them as opaque JSON: `GET /api/v1/whiteboards/{id}` returns the scene; `PUT` validates that it is well-formed excalidraw JSON (`type === "excalidraw"`, `elements` is an array) before writing. No element-level interpretation server-side in v1.
- The index records path, element count, a content hash, and whether embedded `files` (images) exist.
- Markdown references them with an embed: `![[diagram.excalidraw]]`.
- Web UI: inline read-only render via `@excalidraw/excalidraw` in `viewMode`, click → `/w/<path>` editor route with a "Save" that `PUT`s the scene. Autosave is debounced (1.5 s) and uses `If-Match`.
- Images pasted into a drawing are base64 in `files` for v1; externalizing them to attachments is deferred (§18).
- SVG/PNG preview caching is **not** in v1 (the chosen option was "separate files", not "separate files + previews").

### 8.3 Attachments (decision #27)

- `POST /api/v1/attachments` (multipart `file`, optional `folder`) writes into the configured attachments folder and returns:

```json
{
  "path": "attachments/2026/kitchen-sketch.png",
  "markdown": "![[attachments/2026/kitchen-sketch.png]]",
  "sha256": "…",
  "size": 184320,
  "mime": "image/png"
}
```

- `name_strategy = "original"`: sanitize the filename, disambiguate collisions with `-1`, `-2`; `"hashed"`: `<sha256[:12]>-<slug>.<ext>` for dedupe.
- Size limit `attachments.max_size_mb` (default 50) → `413` beyond it. MIME sniffed with `http.DetectContentType`; an allowlist is configurable.
- CLI/agents use this endpoint directly (`lewnotes attach ./sketch.png --folder notes/kitchen`), then insert the returned `markdown` themselves.
- Web editor hooks paste and drag-drop to the same endpoint and inserts `markdown` at the caret — that wiring is a UI concern, not a separate API.

### 8.4 Wikilinks (decisions #12/#13)

Supported syntax:

| Form | Meaning |
|------|---------|
| `[[note]]` | Link to a note |
| `[[note\|display text]]` | Aliased link |
| `[[note#Heading]]` | Link to a heading |
| `[[note#^block-id]]` | Link to a block reference |
| `[[Note]]` | Case-insensitive match |
| `[[folder/note]]` | Partial or full path |
| `![[note]]`, `![[image.png]]`, `![[diagram.excalidraw]]` | Embed |
| `[[missing note]]` | Ghost link (unresolved, first-class) |

**Resolution algorithm** (Obsidian-compatible):

1. Normalize: strip extension, casefold, convert `\` to `/`.
2. Prefer an exact full-path match from the vault root.
3. Else collect all paths whose trailing segments equal the target and pick the **shortest unique** suffix.
4. Zero matches → `unresolved` ghost link; more than one shortest match → `ambiguous` (API returns candidates, UI shows a picker, search reports them under `is:ambiguous`).

**Index output:** every link row keeps `dst_raw`, `alias`, `fragment`, `kind` (`link`/`embed`), `resolved_id` (nullable), and `resolution` (`exact`/`suffix`/`unresolved`/`ambiguous`). Backlinks are the reverse index.

**Renaming.** `POST /api/v1/notes/{id}/move` with `{ "path": "new/path.md", "rewrite_links": true }` (default true):

- Move is atomic (rename within the vault).
- Every inbound link whose form can be safely rewritten is updated: full-path links are rewritten to the new path; bare basename links are rewritten **only if** the old basename would no longer be unique. Embeds follow the same rules.
- Aliases (`[[a|b]]`) and fragments (`#Heading`) are preserved.
- Response reports `{ "moved": true, "links_rewritten": 7, "warnings": [...] }`; partial failure (e.g. an unwritable file) is a warning, not a rollback, and is logged + evented.

**Web title linking.** The web editor treats frontmatter `title` as the document title. Committing a title change writes the title and, when the sanitized matching filename differs, moves the note within its current directory using the same rewrite behavior. Typing does not move the file on every keystroke; the move runs when the title is committed (blur or Enter).

---

## 9. Persistence: SQLite (decision #1)

### 9.1 Driver and pragmas

- **`modernc.org/sqlite`** (pure Go, no CGO, includes FTS5), so the Docker build stays static and cross-compiles. Query code uses `database/sql` so switching drivers is a one-line change.
- DSN pragmas: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=on`, `synchronous=NORMAL`.
- Pool: `MaxOpenConns(1)` for writes plus a small read pool, or a single connection with serialized writes — SQLite's single-writer rule is the constraint; pick the simplest thing (one writable `sql.DB`, `MaxOpenConns(1)`) so long-running API handlers never see `SQLITE_BUSY`.
- DB file: `${LEWNOTES_DATA_DIR}/lewnotes.db` (default `/data`). Never inside the vault (it would pollute the notes).

### 9.2 Rebuildability contract

- Deleting the DB must lose **nothing** except sessions/tokens/preferences. `lewnotes-core` on start with `--rebuild-index` (or an empty DB) rescans the vault, reconstructs notes, links, tags, todos, whiteboards, and attachments.
- IDs are safe across rebuilds only because they live in frontmatter (§7.4). A note without frontmatter gets one assigned and written during the scan.
- Migrations: `PRAGMA user_version` with numbered `migrations/NNNN_*.sql`, applied in a transaction on startup.

### 9.3 Schema sketch

```sql
-- content index
notes(id TEXT PRIMARY KEY, path TEXT NOT NULL UNIQUE, title TEXT,
      mtime INTEGER, size INTEGER, content_hash TEXT, frontmatter JSON,
      excerpt TEXT, word_count INTEGER, deleted_at INTEGER)
links(src_id TEXT, dst_id TEXT, dst_raw TEXT, alias TEXT, fragment TEXT,
      kind TEXT, resolution TEXT, PRIMARY KEY(src_id, dst_raw, kind, fragment))
tags(note_id TEXT, tag TEXT, PRIMARY KEY(note_id, tag))
attachments(path TEXT PRIMARY KEY, sha256 TEXT, size INTEGER, mime TEXT, note_id TEXT)
whiteboards(note_id TEXT PRIMARY KEY, path TEXT, element_count INTEGER, content_hash TEXT)
todos(rowid INTEGER PRIMARY KEY, file_id TEXT, line_no INTEGER, line_hash TEXT,
      raw TEXT, completed INTEGER, completion_date TEXT, priority TEXT,
      creation_date TEXT, description TEXT, due_date TEXT, projects JSON, contexts JSON,
      meta JSON)
todos_fts / notes_fts  -- FTS5 virtual tables (content='notes' / 'todos')
-- app state (NOT rebuildable, and that's fine)
users(id TEXT PRIMARY KEY, username TEXT UNIQUE, password_hash TEXT, created_at INTEGER)
sessions(id TEXT PRIMARY KEY, user_id TEXT, created_at INTEGER, expires_at INTEGER,
         user_agent TEXT, ip TEXT, last_seen_at INTEGER)
api_tokens(id TEXT PRIMARY KEY, name TEXT, token_hash TEXT, prefix TEXT,
           created_at INTEGER, last_used_at INTEGER, revoked_at INTEGER, user_agent TEXT)
meta(key TEXT PRIMARY KEY, value TEXT)  -- index_version, last_scan_at, warnings
```

FTS5 tables use external content + triggers (or are rebuilt on scan) so note edits keep search fresh without double storage. Indexing covers title, body, headings, tags, and (fenced-code-excluded?) body text — plain text indexing of the full markdown body in v1, with the frontmatter block stripped.

### 9.4 Scan behavior

- Startup: if DB empty or `index.version` mismatched → full scan. Otherwise → hash-based incremental scan of `mtime`/`size` changes.
- Concurrency: scan runs in a background goroutine with a progress record in `meta`; the API serves stale data with a `503`-free "indexing" banner state (`GET /api/v1/index/status` → `{state:"scanning", files_done, files_total}`) and SSE `index.scan.*` events.
- `.md` files without frontmatter get an ID written during the scan (this is the one place a read-triggered write happens; it's idempotent and retried).

---

## 10. HTTP API (decision #14)

### 10.1 Conventions

- Base: `/api/v1`. JSON only, UTF-8. Timestamps RFC 3339 UTC.
- Errors: `application/problem+json` (RFC 9457): `{type, title, status, detail, instance, errors[]}`.
- Auth: cookie session (web) or `Authorization: Bearer <token>` (CLI/TUI/agents). Every route except `/api/v1/health`, `/api/v1/auth/setup` (pre-setup), `/api/v1/auth/login`, and the SPA shell requires auth.
- OpenAPI 3.1 document is hand-written at `server/internal/api/openapi.json`, embedded and served at `/api/v1/openapi.json`. A Go test fails when a registered route is missing from it. Generated TS/Go clients are deferred to the CLI slice; the web client is a small typed wrapper.
- Pagination: `?limit=&cursor=` for lists; `?limit=0` means ids only.
- Capability discovery: `GET /api/v1/meta` → `{version, instance_name, limits:{max_note_bytes}, features:["notes","wikilinks","search","tree","directories","sse"]}` so clients negotiate features instead of guessing. The feature list grows as slices land.

### 10.2 Notes

| Method | Path | Notes |
|---|---|---|
| `GET` | `/notes` | List/filter: `path_prefix`, `tag`, `sort=updated\|path\|title`, `limit`, `cursor` |
| `POST` | `/notes` | `{path, content?, frontmatter?}` → creates, injects `id`, returns note + `ETag` |
| `GET` | `/notes/{id}` | `?format=markdown\|json`; returns `ETag`, `content_hash`, `frontmatter`, links, backlinks, tags |
| `GET` | `/notes/by-path?path=` | Convenience lookup (Obsidian-style addressing) |
| `PUT` | `/notes/{id}` | Full replace; **requires `If-Match`**; `412` on mismatch |
| `PATCH` | `/notes/{id}` | Ops-based edits: `{ops:[{type:"append"}\|{type:"prepend"}\|{type:"replace_range"}\|{type:"set_frontmatter"}]}`; requires `If-Match`; patch ops keep agent edits cheap |
| `POST` | `/notes/{id}/move` | `{path, rewrite_links:true}` (§8.4) |
| `DELETE` | `/notes/{id}` | Soft-delete to trash; `If-Match` required |
| `GET` | `/notes/{id}/backlinks` | `[{id, path, line, fragment}]` |
| `GET` | `/notes/{id}/links` | Outgoing links with resolution state |

### 10.3 Tree, files, attachments

| Method | Path | Notes |
|---|---|---|
| `GET` | `/tree?path=&depth=` | Directory listing for sidebar; includes non-md files with type |
| `POST` | `/dirs` | `{path}` → create a directory; relative path, hidden segments rejected |
| `POST` | `/dirs/move` | `{path, new_path, rewrite_links:true}` → move a directory, reindex contained notes, rewrite full-path links |
| `GET` | `/files/{path}` | Raw bytes + `Content-Type`/`ETag`/`Content-Disposition`; images inline, others attachment |
| `HEAD` | `/files/{path}` | Existence/metadata |
| `POST` | `/attachments` | Multipart upload (§8.3) |
| `GET` | `/attachments` | List with size/mime, `orphaned=true` filter |

### 10.4 Todos

| Method | Path | Notes |
|---|---|---|
| `GET` | `/todos` | Filters: `status=open\|done\|all`, `project`, `context`, `priority`, `due_before`, `file`, `q`; returns `{path, line, line_hash, raw, fields}` |
| `POST` | `/todos` | `{file, line?, text, above?}` — appends by default; server parses the text, applies todo.txt normalization only to the tokens it adds |
| `PATCH` | `/todos` | `{file, line, line_hash, set:{completed?, priority?, due?, description?}, add_projects[], rm_projects[], add_contexts[], rm_contexts[]}`; `409` if `line_hash` stale |
| `POST` | `/todos/move` | `{file, line, line_hash, to_file}` for moving between lists |
| `GET` | `/todos/projects` | Distinct projects + counts |
| `GET` | `/todos/contexts` | Distinct contexts + counts |

### 10.5 Search (decision #25)

`GET /api/v1/search?q=<grammar>&limit=&cursor=` → ranked hits with path, title, snippet (with `<mark>` spans), score, and matched fields.

Grammar (shared by web palette, CLI, TUI):

```
q            := term*
term         := [-](text | tag:v | path:prefix | has:drawing|attachment|embed
                    | todo:open|done|overdue | project:v | context:v
                    | is:unresolved|ambiguous | "quoted phrase")
```

- Free text goes to FTS5 (`bm25` ranking, porter + unicode61 tokenizers), title matches boosted.
- `todo:` hits return todo rows rather than notes (results are typed: `note | todo | attachment | whiteboard`).
- Unknown filters → `400` with the offending token, not silently ignored (agents depend on this).

### 10.6 Whiteboards

`GET /api/v1/whiteboards/{id}` → raw scene; `PUT` with `If-Match`; `POST /api/v1/whiteboards` → `{path}` creates `{type:"excalidraw",version:2,source:"lewnotes",elements:[],appState:{},files:{}}`.

### 10.7 Auth, tokens, admin

| Method | Path | Notes |
|---|---|---|
| `GET` | `/auth/status` | `{setup_required: bool}` unauthenticated |
| `POST` | `/auth/setup` | First-run only: `{password, instance_name}`; refuses if a user exists (and is rate-limited) |
| `POST` | `/auth/login` | `{password}` → sets `HttpOnly` session cookie; generic error text |
| `POST` | `/auth/logout` | Revokes session |
| `GET` | `/auth/me` | `{username, auth:"session"\|"token", token:{id,name}}` |
| `POST` | `/auth/password` | Rotate password; invalidates all sessions and (opt-in) tokens |
| `GET` | `/tokens` | Device list: name, prefix, created, last_used, user agent |
| `POST` | `/tokens` | `{name}` → **returns the encoded token once** (see §11.3) |
| `DELETE` | `/tokens/{id}` | Revoke |
| `POST` | `/index/rescan` | `{full:bool}` → starts background scan |
| `GET` | `/index/status` | Progress, drift warnings, ambiguous links |
| `GET` | `/events` | SSE (§10.8) |
| `GET` | `/health` | `{ok, version, uptime}` (no auth, no data) |

### 10.8 SSE change events

`GET /api/v1/events` (auth via cookie or bearer; `?token=` allowed only for EventSource clients, documented as less safe).

Event shape:

```
id: 01JC9A...
event: note.updated
data: {"id":"01JC8...","path":"notes/kitchen.md","etag":"\"abc\"","origin":"cli"}
```

Types: `note.created|updated|moved|deleted`, `todo.created|updated|deleted`, `whiteboard.updated`, `attachment.created`, `index.scan.started|progress|finished`, `session.revoked`.

Mechanics:

- In-memory ring buffer (default 1000 events) enables `Last-Event-ID` replay; on overflow the server sends `event: resync` and the client refetches.
- 20 s heartbeat comment frames to survive proxies.
- `origin` (`web|cli|tui|agent|core`) comes from a client header `X-Lewnotes-Client` so a client can ignore its own echo.
- The web UI uses this to refresh sidebar, open editor, and search results without polling.

### 10.9 Concurrency model (decision #15)

- `ETag` on notes/todos/whiteboards is `"<sha256 of content>"` (strong).
- Mutating methods require `If-Match`; missing → `428 Precondition Required` (documented, with a `GET` hint), stale → `412 Precondition Failed` including the current ETag and a `diff` hint endpoint.
- The S4b conflict panel fetches the current note and computes the diff in the browser (read-only, no merge). A server-side `GET /notes/{id}?diff_base=<etag>` shortcut remains an open optimization.
- Whole-file replace is the default write; `PATCH` ops exist so an agent appending a line does not need to resend an entire note.

---

## 11. Authentication & credentials (decisions #5–#8)

### 11.1 Model

Single account. Two credential types, one authorization check:

- **Session** (web): opaque 32-byte session ID in an `HttpOnly; Secure; SameSite=Lax; Path=/` cookie, 30-day sliding expiry, row in `sessions`, revoked on logout/password change. The database stores `sha256(session_id)` rather than the cookie value, so a leaked database does not hand over live sessions; each session also carries a CSRF token compared against `X-Lewnotes-CSRF` on cookie-authenticated mutations.
- **Device token** (CLI/TUI/agents): long-lived bearer token, hashed at rest, listed and revoked in the web UI.

Both resolve to the same `user_id`; there are no roles. CSRF: `SameSite=Lax` plus a double-submit token header (`X-Lewnotes-CSRF`) on state-changing cookie-authenticated requests. CORS: same-origin only by default, `auth.allowed_origins` for exceptions.

### 11.2 First run

1. `GET /api/v1/auth/status` → `setup_required: true` when `users` is empty.
2. Web shows a setup screen: instance name + password (+ confirm), with a strength meter.
3. `POST /auth/setup` creates the user with argon2id (`m=64MiB, t=3, p=2`, 16-byte salt, 32-byte key) inside a transaction that re-checks emptiness, so a race cannot create two owners.
4. Setup is then permanently disabled (`setup_required:false`); CLI/TUI in an unconfigured instance print a "visit the web UI to finish setup" message rather than offering setup themselves.

Why not CLI setup: the web UI is the trust root and the only place a browser TLS certificate is visible to the owner.

### 11.3 The device token format — and the "where is the server?" problem

A token must carry both the endpoint and the secret, because self-hosted instances have no canonical URL. Format:

```
lnt.<base64url(server_url)>.<token_id_ulid>.<secret_base64url>
```

Example (illustrative):

```
lnt.aHR0cHM6Ly9ub3Rlcy5leGFtcGxlLmNvbQ.01JC8Z9K2M4N6P8Q0R2S4T6V8W.kJ8s...32bytes...
```

- `.` is the separator; it never appears in base64url or Crockford base32, so splitting is unambiguous.
- `server_url` is the absolute origin the web UI was served from at creation (respecting `X-Forwarded-Proto`/`Host` when `trust_proxy` is on), e.g. `https://notes.example.com`.
- `token_id` is public (used for lookup and revocation display); `secret` is 32 random bytes.
- Server stores `sha256(secret)` and a display prefix `lnt.aHR0…k8s`, never the plaintext token. Verification is constant-time.
- The token is a **bearer credential**: pasting it into a chat leaks it. Mitigations: shown once, revocable, `last_used_at` + user agent visible, optional `--keyring` storage, and docs telling agents to use `LEWNOTES_TOKEN` from the environment rather than literals.
- If the instance's public URL changes, tokens still work (the server ignores the embedded URL); only the client's stored endpoint needs updating (`lewnotes config set server <url>`). The CLI warns when the embedded URL differs from its configured one.
- Login handoff (S5b) uses a separate, versioned envelope: `lnh1.<base64url(envelope)>`, where the envelope is AES-256-GCM ciphertext of `{server, token, token_id, device_name, username}` encrypted to an ephemeral X25519 key generated by the CLI for that login. It is not a credential format and is never stored by a client. This is deliberately not a JWT: a JWT is signed but readable, and the point is to hide the endpoint and credential from anyone who captures the blob.

### 11.4 `lewnotes login` walkthrough

> **Superseded by the S5b handoff flow** (`.agents/plans/2026-11-25-cli-login-handoff.md`). The
> default `lewnotes login` now opens the web app and completes through an encrypted, single-use
> handoff with no typed secret. The paste flow below remains supported as the manual/break-glass
> path, and `--token-stdin` / `LEWNOTES_TOKEN` remain the agent and CI paths.

```
$ lewnotes login
No server configured yet.

Server URL (e.g. https://notes.example.com): https://notes.example.com

Checking https://notes.example.com ... lewnotes 0.1.0, vault "home", setup complete ✓

To authorize this machine:
  1. Open  https://notes.example.com/settings/tokens
  2. Sign in with your password.
  3. Create a token named  caleb-laptop  (press Enter to accept this name).
  4. Copy the token and paste it below.

Open that page now? [Y/n] y
Waiting for token... (paste it, or press Ctrl-C to cancel)
Token: lnt.aHR0…k8s

✓ Verified against https://notes.example.com as "caleb"
✓ Stored credential in ~/.config/lewnotes/config.toml (0600)

Try:  lewnotes note new "Hello" --body "first note" --json
```

Flow rules:

- Server URL resolution order: `--server` flag → `LEWNOTES_SERVER` → existing config → interactive prompt. The URL is validated by `GET /api/v1/health` before proceeding (shows version, vault name, and whether setup is complete).
- Browser opening is best-effort (`--open`/`--no-open`, default: try, fall back to printing).
- Token input is read without echo when the terminal is a TTY; `--token-stdin` reads stdin for scripts/containers; `--token` exists but is discouraged/documented as shell-history-unsafe.
- On paste, the CLI decodes the embedded URL; if it differs from the configured server it asks which one to trust and defaults to the embedded value.
- Verification is `GET /api/v1/auth/me`; on failure the token is not stored.
- Fully non-interactive: `lewnotes login --server https://… --token-stdin --json` (agents/CI).
- Token storage: `~/.config/lewnotes/config.toml` with `0600`, or the OS keyring with `--keyring`. Env var `LEWNOTES_TOKEN` overrides stored config (preferred for agents). `lewnotes logout` deletes the local credential and, with `--revoke`, calls the API to revoke that token server-side.
- The CLI/TUI share this one implementation because they are the same binary.

### 11.5 Web token page

`/settings/tokens`: device list table (name, prefix, created, last used, client), "Create token" dialog that shows the full token once with a copy button, a warning that it is unrecoverable, and per-row "Revoke". Also `DEVICE NAME` is prefilled from the browser/OS. The normal path is `lewnotes login`, which creates a token here after browser approval; the page copy leads with that.

`/settings/instance` (S5b): the effective public URL — the origin embedded in tokens and login links. The owner types or accepts the browser's origin, and **Test** pings `GET <url>/api/v1/health` before saving, reporting the version, instance name, and whether setup is complete. `server.public_url` / `LEWNOTES_PUBLIC_URL` in config wins and renders read-only; otherwise the value lives in the `settings` table. The page also shows `server.listen` and `auth.trust_proxy`, and a **Connect a machine** block with the copyable `lewnotes login <public-url>` command.

### 11.6 Hardening checklist

- Rate limits: login 5/min/IP with exponential backoff, token verification 30/min/IP, uploads 20/min, general 600/min/token.
- Uniform failure messages and constant-time comparisons; argon2id parameters in config.
- Password change revokes all sessions and optionally all tokens.
- TLS expected at a reverse proxy; `auth.cookie_secure` can be set false only for local dev, and `auth.trust_proxy` must be explicitly enabled before honoring forwarded headers.
- Security headers on the SPA: CSP (no inline script except the Svelte bootstrap nonce), `X-Content-Type-Options`, `Referrer-Policy: no-referrer`, `Permissions-Policy`.
- `GET /health` leaks nothing beyond version.

---

## 12. Web UI (decisions #19/#21)

### 12.1 Stack

- **SvelteKit 5** in SPA mode (`adapter-static` with fallback), embedded into the Go binary with `go:embed` and served from `/`. No Node runtime in the container.
- **Svelte 5 runes** for state; no store-heavy legacy patterns.
- **shadcn-svelte** components on **Tailwind 4**; dark-first with a light theme.
- **Editor:** S4b ships a plain textarea with Write/Preview tabs and a marked + DOMPurify preview. **CodeMirror 6** (markdown language, wikilink autocomplete, `[[` completion backed by `GET /notes?limit=20&q=`), remark/rehype preview, and optional KaTeX remain follow-ups.
- **TanStack Query (svelte-query)** or a small hand-rolled fetch layer driven by the generated OpenAPI client; SSE invalidates queries.
- Excalidraw loaded lazily on whiteboard routes only (it is heavy).

### 12.2 Screens

| Route | Purpose |
|---|---|
| `/setup` | First-run password + instance name |
| `/login` | Password sign-in |
| `/` | Note list/tree + last-edited, quick capture box |
| `/n/<path>` | Note view/edit: editor ⇄ preview, backlinks panel, tags, "move/rename" dialog, conflict banner on `412` |
| `/todos` | Todo views: by project, context, priority, due; inline complete/toggle; mobile-friendly list |
| `/w/<path>` | Excalidraw editor |
| `/search` | Full result page; also ⌘K palette overlay |
| `/settings` | Devices/tokens, password change, instance info, index status/rescan |
| `/settings/tokens` | The token issuance page used by the CLI flow |
| `/settings/instance` | Public URL + health validation + "Connect a machine" command |
| `/cli` | Headless device-code entry; resolves and forwards to the authorize panel |
| `/cli/authorize` | Browser approval panel for a pending CLI login |

### 12.3 Behavior

- Keyboard-first: ⌘K palette, `e` edit, `⌘S` save, `Esc` cancel, `/` search — documented in `DESIGN.md`. `g n` / `g t` navigation is deferred.
- Mobile-tolerant: below `md`, sidebar collapses to a drawer, editor goes full-width, a persistent "＋ capture" button opens note/todo quick-add, and the todos view uses large touch targets and swipe-to-complete.
- Server-confirmed writes with ETags; on `412` show a diff panel ("server changed this note") with Reload / Overwrite options — no automatic merge.
- Empty states for empty vault, no results, unresolved links, and indexing-in-progress.
- Ghost links render in a dashed accent color and click-create the note at the connected component's chosen path.

### 12.4 `DESIGN.md` deliverable (M0)

Must be written before UI implementation and contain:

1. Design principles (quiet, content-first, keyboard > mouse, no chrome that competes with text).
2. Token tables: color (semantic + neutral ramp, dark/light), type scale, spacing scale, radii, shadows, motion durations/easings.
3. Layout grid and breakpoints, sidebar widths, editor measure (~72ch).
4. Component inventory built on shadcn-svelte (Button, Dialog, Command/Palette, Sheet, Tabs, Toast, Badge, Tooltip, Input, Select, Table) with usage rules and the "never fork a primitive" rule.
5. Interaction patterns: autosave cadence, conflict UI, destructive confirmations, loading/skeleton rules, error copy voice.
6. Keyboard map and focus-visible rules.
7. Accessibility baseline: WCAG AA contrast, full keyboard paths, ARIA for the palette and dialogs, reduced-motion support.
8. ASCII wireframes for each screen, including the mobile breakpoint.
9. Editor typography spec (font stack, heading scale, code blocks, task list rendering).

---

## 13. CLI (decisions #8/#24)

Single binary `lewnotes`, TUI included as a subcommand. Every command supports `--json`.

### 13.1 Command surface (v1)

```
lewnotes login | logout | whoami | config get|set
lewnotes note list [--path-prefix] [--tag] [--sort] [--json]
lewnotes note show <ref>            # ref = id, path, or [[wikilink]] text
lewnotes note new <title|path> [--body|--body-file|-] [--folder] [--json]
lewnotes note append <ref> [--text|-]   # append paragraph/bullet
lewnotes note edit <ref>            # $EDITOR round-trip using If-Match
lewnotes note rm <ref> [--purge]
lewnotes note mv <ref> <new-path> [--no-rewrite-links]
lewnotes note links <ref> | backlinks <ref>
lewnotes search <query> [--limit] [--type note|todo|attachment]
lewnotes todo list [--status] [--project] [--context] [--priority] [--due-before]
lewnotes todo add <text> [--file todo.md] [--above]
lewnotes todo done <file>:<line> [--line-hash]      # or interactive picker
lewnotes todo undone <file>:<line>
lewnotes todo edit <file>:<line> --set due=2026-01-12 --add-context errands
lewnotes todo projects | contexts
lewnotes attach <file> [--folder]
lewnotes board list|new <path>|show <ref>
lewnotes index rescan [--full] | index status
lewnotes tui
lewnotes skills print            # emits the agent SKILL.md to stdout
lewnotes completion bash|zsh|fish
```

Global flags: `--server`, `--token`, `--config`, `--json`, `--no-color`, `--quiet`, `--verbose`, `--timeout`.

`<ref>` resolution: exact note ID → exact vault path → unique path suffix → unique title match; ambiguous refs fail with the candidate list (exit code 3).

### 13.2 Agent ergonomics

- `--json` output is stable and documented: `{ok, data, warnings[], error?}`, one object per command (or NDJSON with `--json=lines` for lists).
- `--help --json` prints the **entire command tree** with, per command: name, summary, arguments, flags (type, default, required), output shape keys, an `examples` array (including JSON examples), and mutation/read-only classification. This is the "self-describing CLI" contract; it is generated from the same command metadata the help renderer uses, so it cannot drift.
- Exit codes: `0` ok, `1` generic failure, `2` usage error, `3` ambiguous reference, `4` auth failure, `5` precondition/conflict (`412`/`409`), `6` server unreachable.
- Non-interactive by default when `--json` is set: never prompt, never open a browser, never use a pager.
- `lewnotes skills print` (or `lewnotes skills install --dir .agents/skills`) ships a `lewnotes-cli` skill: when to use lewnotes, the login env-var pattern, read-before-write, `If-Match`/ETag handling, ref syntax, todo addressing, and safety rules (no bulk deletes, prefer `note append`/`PATCH` over full rewrites, always pass `--json`).
- Destructive commands require `--yes` when stdin is not a TTY.

### 13.3 TUI (decision #19)

Full reader/editor, not just capture:

- **Bubble Tea** (charmbracelet) with `bubbles` components; `glamour` for rendered markdown; a textarea-based editor pane with vim-ish optional keybindings.
- Layout: sidebar (tree, fuzzy filter) | editor/preview | right panel (backlinks, tags, links).
- Fuzzy finder (`/` or `ctrl+k`), note switching without refetch storms, todo pane with inline completion, search results with preview.
- Editing goes through the same client and `If-Match`; conflicts surface as a modal with reload/overwrite.
- Excalidraw is out of scope in the terminal: `o` opens the drawing's URL in the browser.
- Config: `theme`, `keymap` (`default`/`vim`), `editor.line_numbers`, `start_view` (`notes`/`todos`).

---

## 14. Agent-facing repo conventions (decision #20)

```
AGENTS.md                      # canonical entry point
.agents/
├── skills/<name>/SKILL.md     # frontmatter: name, description; body: instructions
├── rules/*.md                 # short, imperative project rules
├── plans/YYYY-MM-DD-<slug>.md # written before multi-step work
└── walkthroughs/YYYY-MM-DD-<slug>.md  # written after, describing what happened
```

`AGENTS.md` must cover: what lewnotes is, the two modules and how to build/run/test them, the Docker workflow, the "files are truth / DB is an index" invariant, the byte-fidelity rule for todo.txt and frontmatter, the requirement that every UI feature have an API endpoint, where plans and walkthroughs go, and how to add a skill.

Initial rules to author in M0:

| Rule file | Content |
|---|---|
| `rules/architecture.md` | No server imports from `cli/client`; API-first; no UI-only state that matters; SQLite is disposable. |
| `rules/file-formats.md` | Never reformat untouched lines; golden round-trip tests; frontmatter preservation; wikilink rewrite rules. |
| `rules/api.md` | `problem+json`, `If-Match` on mutations, OpenAPI regenerated in the same commit as handler changes, SSE event naming. |
| `rules/go.md` | Error wrapping conventions, context-first, `internal/` boundaries, table tests, no global mutable state, `golangci-lint` config. |
| `rules/secrets.md` | Never log tokens; hash at rest; no secrets in vault or repo; `.env` not committed. |

Initial skills: `lewnotes-cli` (shipped with the binary), `lewnotes-vault-formats` (todo.txt/frontmatter/link grammar with worked examples), `lewnotes-api` (endpoint recipes).

---

## 15. Deployment

### 15.1 Container

- Multi-stage Dockerfile: `node:22` builds `web/` (static) → `golang:1.2x` builds `lewnotes-core` with the web dist embedded → runtime `gcr.io/distroless/static` (or `alpine` if a shell is needed for ops).
- Runs as non-root (`UID 65532` or `1000`); `/data` volume holds vault + DB; `/data` must be writable.
- Salted env: `LEWNOTES_VAULT=/data/vault`, `LEWNOTES_DATA_DIR=/data`, `LEWNOTES_LISTEN=:8080`.
- `HEALTHCHECK` hits `/api/v1/health`. Image tagged `latest` + semver; multi-arch `amd64`/`arm64`.
- No CGO → the build needs no C toolchain and cross-compiles cleanly (the reason for `modernc.org/sqlite`).

### 15.2 Compose example

```yaml
services:
  lewnotes:
    image: ghcr.io/<owner>/lewnotes:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      LEWNOTES_VAULT: /data/vault
      LEWNOTES_DATA_DIR: /data
      LEWNOTES_TRUST_PROXY: "true"
    volumes:
      - ./vault:/data/vault
      - ./db:/data
```

### 15.3 Config precedence

Flags (server binary) > `LEWNOTES_*` env > `lewnotes.toml` > defaults. Full example config checked in as `lewnotes.example.toml`, documenting every key with its default. Validation errors name the key and the source (env vs file) at startup.

```toml
[server]
listen = ":8080"
data_dir = "/data"
trust_proxy = false
public_url = ""            # only used to display/embed URLs when proxy headers are unavailable
log_level = "info"

[vault]
path = "/data/vault"
inbox = "inbox"
attachments = "attachments"
ignore = ["**/.trash/**"]

[index]
reconcile_interval = "0s"

[todos]
globs = ["todo.md", "todos/**/*.md"]
archive_completed = false

[search]
snippet_length = 32

[attachments]
max_size_mb = 50
allowed_mime = ["image/*", "application/pdf", "text/*"]
name_strategy = "original"

[auth]
session_ttl = "720h"
token_last_used_interval = "1h"
cookie_secure = true
allowed_origins = []
```

### 15.4 Backup/ops

- Backing up the vault directory is sufficient for content; back up `lewnotes.db` too to keep tokens/sessions (or accept re-login).
- `GET /api/v1/index/status` and structured logs (`slog`, JSON) are the observability story; document `docker logs` + optional Prometheus metrics as future work.
- WAL checkpoint on shutdown; DB backup via `VACUUM INTO` behind an admin endpoint is deferred.

---

## 16. Milestones

| Milestone | Deliverable | Done when |
|---|---|---|
| **M0** | Scaffolding: `go.work`, empty modules, Dockerfile skeleton, `AGENTS.md`, `.agents/` tree with rules, `DESIGN.md`, this spec committed | `docker compose up` serves a placeholder and `go build ./...` passes |
| **M1** | Server core: config, path-safe vault, frontmatter IDs, scan, SQLite schema + migrations, note CRUD with ETags, link resolution, FTS5 search | Integration tests over a fixture vault; rebuild-from-empty-index test passes |
| **M2** | API: OpenAPI generation, problem+json, SSE hub, index status/rescan, attachments | `openapi.json` validated; SSE replay test with `Last-Event-ID` |
| **M3** | Auth: setup, sessions, device tokens, token encoding, rate limits, `/settings/tokens` backend | Non-interactive login test against `httptest`; token never logged |
| **M4** | CLI: client package, login walkthrough, note/search/attach commands, `--json`, `--help --json`, skills print | Golden JSON output tests; agent skill installed into `.agents/skills` |
| **M5** | Web UI: SPA shell embedded, notes tree/editor/preview, backlinks, ⌘K search, login/setup, tokens page, SSE live updates | Playwright smoke: create → edit → conflict → search |
| **M6** | Todos: parser/serializer with byte-fidelity goldens, todo API, todo CLI, todo web view | Round-trip property test over a corpus of odd lines |
| **M7** | Whiteboards: storage API, inline render, editor route | Save/load round-trip of a fixture scene |
| **M8** | TUI: read/edit/search/todos | Manual script of key paths; no API-only features in TUI |
| **M9** | Hardening: docs, backups, security headers, release pipeline | First tagged image |

---

## 17. Testing & quality gates

- **Go:** table-driven unit tests per internal package; golden files for todo.txt lines, frontmatter insertion, and wikilink rewriting (`testdata/`); `httptest` API tests with an in-memory SQLite and `t.TempDir()` vaults; one test that deletes the DB and asserts identical API output after rescan.
- **Property test:** `parse(line) → serialize` is byte-identical when no mutation is applied (especially whitespace, unicode, unknown `key:value`).
- **Web:** component tests for the editor link plugin; one Playwright smoke path; no broad E2E matrix in v1.
- **Lint:** `golangci-lint`, `prettier`/`eslint` for web, `go vet`; CI runs build + tests + OpenAPI regeneration diff (fail if generated files are stale).
- **Non-negotiables:** no secrets in logs or fixtures, no path traversal, no unauthenticated data routes. Each has a test.

---

## 18. Open questions (deliberately unresolved)

1. **Token scopes.** ~~Long-lived tokens have full access; a read-only scope for agents and a todo-only scope would reduce blast radius.~~ **Resolved in S2 (v1):** all tokens are full-access. `api_tokens.scope` exists, defaults to `full`, and is returned by `/auth/me` and `/tokens`, so adding `read`/`todos` scopes later is a middleware change rather than a migration. Deferred until a client actually needs a narrower scope.
2. **Keyring vs plaintext config** for CLI credential storage on headless servers.
3. **Soft delete vs hard delete** and trash retention policy (§7.6).
4. **Git history:** optional auto-commit per write (`history.git = true`) is attractive for a file vault but adds lock/performance complexity.
5. **Todo identity beyond `(file, line)`:** should the server support an optional `id:<ulid>` key:value tag for stable references at the cost of format purity?
6. **Attachment dedupe:** content-hashed naming vs original names by default.
7. **Excalidraw image externalization** to attachments instead of base64 in `files`.
8. **Instance URL changes:** token format embeds the URL; do we need a migration tool for clients after a domain change?
9. **OIDC later:** the chosen auth option allows adding an IdP; confirm the interface seam (`auth.Authenticator`).
10. **Indexing scope:** strip fenced code blocks and frontmatter from FTS? Index attachments' text (PDF)?
11. **Rate-limit storage:** in-memory per-process vs SQLite-backed (matters with multiple replicas — currently single instance, so in-memory).
12. **Vault import:** ingesting an existing Obsidian vault (adding frontmatter IDs in bulk, converting `[[links]]`) is out of v1 but a likely v1.1.
13. **Tablet/phone PWA:** manifest + installable shell could be cheap; not planned.
14. **Search ranking tuning** and whether title/path matches should outrank body (`bm25` weights).
15. **Daily notes / templates / quick-capture target**: new notes are created inside `inbox/`, but the daily-note/template workflow and filename scheme (timestamped vs prompt) are unspecified.
16. **Licensing** (MIT/Apache-2.0) and repo hosting details (`ghcr.io/<owner>` placeholder).

---

## 19. Acceptance criteria (v1)

1. `docker run` with one volume → setup screen → create owner password → create a note in the web UI → read it back with `lewnotes note show` on another machine after `lewnotes login`.
2. Every web UI capability used in the acceptance path has a documented endpoint in `openapi.yaml`, and the document regenerates cleanly in CI.
3. Deleting `lewnotes.db` and restarting yields the same notes, links, todos (when enabled), and search results; only sessions/tokens are lost.
4. `note.md` containing `[[Missing Note]]` shows as a ghost link, is creatable in one click, and the backlink appears immediately in the original note.
5. Renaming a note updates inbound links in the same request and reports how many were rewritten.
6. Two concurrent writes: the stale one gets `412` and the web UI shows a diff-based conflict panel; no data is silently lost.
7. A todo line written by hand in `todo.md` is visible in `GET /api/v1/todos`, and toggling it done produces a byte-identical line except for the `x <date>` prefix and removed priority.
8. `lewnotes --help --json` fully describes the CLI, and an agent can complete "append a line to today's note" using only that output plus `--json` responses.
9. No route returns vault data without a valid session or bearer token; login is rate-limited; tokens are stored hashed.
10. `AGENTS.md` and `.agents/{skills,rules,plans,walkthroughs}` exist, with at least one rule file per concern and a plan/walkthrough template.

---

## 20. Appendix: references consulted

- todo.txt format primer — <https://github.com/todotxt/todo.txt>
- Excalidraw JSON schema — <https://docs.excalidraw.com/docs/codebase/json-schema>
- Excalidraw integration (embedding the editor) — <https://docs.excalidraw.com/docs/@excalidraw/excalidraw/integration>
- RFC 8628 device authorization grant — <https://www.rfc-editor.org/rfc/rfc8628>
- Task attribution / CLI auth flows — <https://workos.com/blog/cli-auth>
- `modernc.org/sqlite` — <https://pkg.go.dev/modernc.org/sqlite>
- SQLite FTS5 — <https://sqlite.org/fts5.html>
- RFC 9457 problem details (used for API errors)
