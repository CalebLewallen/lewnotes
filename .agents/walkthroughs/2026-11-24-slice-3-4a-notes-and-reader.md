# Walkthrough — S3 notes core + S4a read-only web

**Date:** 2026-11-24 · **Slices:** S3 (notes core) and S4a (login + read-only browsing)
**Plan:** [`.agents/plans/2026-11-24-build-slices.md`](../plans/2026-11-24-build-slices.md)

## What changed

### Notes core (S3)

- `migrations/0003_links.sql`: link line numbers + ambiguity candidates, plus reverse indexes.
- `internal/links`: parses `[[target|alias#fragment]]` and `![[embed]]`, ignores escaped openers, resolves exact path → unique suffix → unresolved/ambiguous with sorted candidates.
- `internal/notes`: the single mutation path for create/read/list/update/patch/move/delete.
  - Content is written through `vault.WriteAtomic`; `id:` is injected server-side and cannot be changed by a client write.
  - ETags are strong `sha256(content)`; missing `If-Match` → `428`, stale → `412` with the current ETag and **no file change**.
  - PATCH supports `append`, `prepend`, `replace_range`, `set_frontmatter`.
  - Delete moves to `.lewnotes/trash/<timestamp>-<name>`.
  - Move renames atomically, rewrites inbound full-path links, rewrites bare links only when the old basename uniquely identified the note, preserves aliases/fragments, and reports partial failures as warnings.
- `index`: note lookups/listing, link replacement, backlinks, FTS search (title boost, `<mark>` snippets, tag/path/resolution/kind filters), path moves, and link indexing in the scanner (resolved after the full walk so rebuilds match live edits).
- `internal/search`: shared grammar (`tag:`, `path:`, `is:unresolved|ambiguous`, `has:embed`, negation, quoted phrases); unknown/unavailable filters are `400`s.
- `internal/events`: bounded ring, atomic replay subscription, `Last-Event-ID`, resync on wrap/overflow, SSE heartbeat.
- API: notes CRUD/by-path/move/links/backlinks, tree, raw files with ETag/304, search, events, meta; all default-deny except the documented public routes.
- `server/internal/api/openapi.json` embedded at `/api/v1/openapi.json`; a test fails if a registered route is missing.
- `server/testdata/scripts/notes.sh`: live auth + notes + links + ETag + search smoke that never prints credentials.

### Web (S4a)

- `DESIGN.md`: dark-first tokens, type/spacing rules, component inventory, breakpoints, exclusions.
- `web/src/lib/api.ts`: typed fetch wrapper with credentials, CSRF header on mutations, RFC 9457 error surfaces.
- `web/src/lib/markdown.ts`: `marked` → DOMPurify, wikilink → app route based on the API's resolution, unresolved links rendered as dashed ghosts, image embeds through `/api/v1/files`.
- Routes: `/setup`, `/login`, guarded `(app)` shell with sidebar/tree/filter and mobile drawer, `/notes` recent list, `/n/[...path]` reader with backlinks and unresolved links, logout, themed error page.
- Hand-built inputs/buttons instead of the shadcn CLI; SvelteKit 3 uses `#lib` package imports (with a matching TS path alias).

## Verification actually run

- `cd server && gofmt -l . && go vet ./... && go test ./...` — pass.
- `go test -race ./...` — pass.
- `cd web && npm run check && npm run build` — 0 errors/warnings; static build written to `web/build`.
- Unit/API coverage includes: link parse/resolve tables, ghost links, ETag `428`/`412` with unchanged file, create-ID injection, PATCH append, move rewrite counts and preserved aliases, delete-to-trash, tree/types, rebuild IDs and link rows, FTS search, SSE replay/resync, OpenAPI route coverage, default-deny on new routes.
- Live: clean vault → owner setup → `notes.sh` passed (create, resolved + ghost links, `428`, `412`, update, PATCH, tree, search by run id, backlinks, delete).
- Live dev proxy: `/`, `/login`, `/setup`, and `/n/inbox/Welcome.md` return the SPA; `/api/v1/health` through Vite succeeded.
- Log scan after the live run: 0 matches for the password or `lnt.` tokens; 0 server errors; 0 web errors.

## Decisions and limitations

- **OpenAPI:** hand-written + route-coverage test instead of adopting huma. Spec §10.1 and the plan record this.
- **Link states:** resolved links are `exact` or `suffix` (per spec); the smoke accepts either.
- **Link table key:** `(src, dst_raw, kind, fragment)` collapses two links with different aliases to one row; rewriting still updates both occurrences in the file.
- **No browser automation:** Chrome is unavailable here, so the SPA was verified by typecheck/build and live HTTP routes, not by clicking. The manual visual/mobile test is the user's steering step.
- **Wikilink rendering is textual:** the reader replaces `[[…]]` before markdown parsing, so a literal wikilink inside a fenced code block is also rewritten. A marked tokenizer extension should fix that with the editor slice.
- **Not yet:** editor/autosave, search palette, rename/ghost creation from the reader, conflict diff, SSE client refresh, SPA embedding in Go, todo/whiteboard features.

## Next

S4b after steering: editor with explicit save/autosave and `If-Match`, ⌘K search, rename + ghost-link flows, 412 conflict diff, SSE refresh, then `go:embed` the built SPA and serve it from `lewnotes-core`.
