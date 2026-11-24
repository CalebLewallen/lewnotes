# Slice 1 — Core plumbing: config, vault, SQLite, scan

**Date:** 2026-11-24 · **Plan:** [S1](../plans/2026-11-24-build-slices.md)

## What was built

| Package | Responsibility |
|---|---|
| `internal/config` | TOML + `LEWNOTES_*` env + defaults, unknown-key rejection, validation errors naming key and source, absolute path normalization |
| `internal/vault` | Vault-relative path resolution (traversal, absolute, `.lewnotes`, symlink escape rejected), atomic fsync+rename writes serialized per path, ignored/dot-dir walks |
| `internal/markdown` | Frontmatter split/parse, surgical `id:` insertion (append last) and replacement, ULID validation, title (frontmatter → H1 outside fences), excerpt |
| `migrations/0001_init.sql` | notes, links, tags, meta, `notes_fts` (FTS5) |
| `internal/index` | Open/pragmas/migrations, note upsert + tags + FTS, delete, meta, scan status; scanner with incremental diff, ID assignment, duplicate reassignment, stale removal |
| `internal/api` | `GET /api/v1/index/status`, `POST /api/v1/index/rescan` (202, background, 409 if running) |
| `cmd/lewnotes-core` | `-config`, `-vault`, `-data-dir`, `-rebuild-index`; startup scan; `inbox/` + `attachments/` scaffolding |

## Decisions made during the slice

- **Migrations live in `server/migrations` as a tiny embedded package** so the repo layout stays as specified while the binary remains self-contained.
- **`database/sql` with `MaxOpenConns(1)`.** One serialized writer avoids `SQLITE_BUSY` entirely; revisit only if read latency becomes a problem.
- **The scanner re-stats a file after writing an ID.** Without this the stored `mtime`/`size` describe the pre-write file, so every incremental scan reindexes every ID-assigned note.
- **Duplicate ID resolution uses `markdown.SetID`**, not `EnsureID` — `EnsureID` correctly refuses to touch a valid ID, which is exactly wrong when the ID is valid but already owned by another path.
- **Malformed YAML frontmatter is skipped with a warning** rather than being rewritten. It surfaces in `index/status.warnings`; a future slice can add a repair path.
- **ID writes set mode `0644`.** Preserving prior file modes is unhandled (noted below).

## Verification

```
cd server && go fmt ./... && go vet ./... && go test ./...
```
All packages pass: config precedence/source errors, path and symlink escapes, atomic write with no temp residue, ignored walks, frontmatter golden pairs (plain, existing block, CRLF, forced replacement), idempotent `EnsureID`, migration version, tag/FTS upserts, incremental skip, stale removal, duplicate reassignment, DB-loss rebuild, and the HTTP status/rescan flow.

Live run (temp vault, temp data dir):

- First boot wrote `id: 01…` into `alpha.md` and `notes/beta.md` and reported `state: idle, notes: 2`.
- `POST /index/rescan {"full":true}` returned `202` and the status returned to idle.
- Deleting the data dir and restarting produced the **same IDs** — the frontmatter contract holds.

## Known gaps / follow-ups

- File modes are not preserved on ID write; low risk until external tooling depends on them.
- `oklog/ulid` accepts some Crockford aliases (e.g. `U`), so `ValidID` is "parses as ULID", not a strict alphabet check.
- `vault.Resolve` converts `\` to `/` rather than rejecting backslashes; revisit if Windows clients send paths.
- `index.reconcile_interval`, FTS querying, link tables, and tag APIs are schema-only so far.
- **The index routes are unauthenticated by design until S2.** Do not expose this build publicly.
- No CI yet; checks are manual commands.

## Next slice

**S2 — auth in core.** Decide token scopes first (spec §18.1), then `0002_auth.sql`, argon2id, sessions + CSRF, the `lnt.` token encode/decode package with round-trip tests, rate limits, and default-deny middleware with an enumerated-route test.
