# Walkthrough — S4b editor, search, conflicts, and embedded SPA

**Date:** 2026-11-24 · **Slice:** S4b
**Plan:** [`../plans/2026-11-24-build-slices.md`](../plans/2026-11-24-build-slices.md)

## What changed

### Web

- **Editor:** `/n/<path>` now toggles between reader and a full-file textarea editor with Write/Preview tabs. Autosave starts 900ms after typing; `⌘S`/`Ctrl-S` saves immediately. Dirty navigation saves before leaving, and refresh/close warns. The note's ETag is sent as `If-Match` on every write.
- **Conflicts:** `412` fetches the current server note and opens a client-side unified line diff with **Reload server version** and **Overwrite with mine**. There is no silent or automatic merge. External SSE changes while clean reload in place; while dirty they show a review banner.
- **Create and ghosts:** unresolved wikilinks are create affordances, and the unresolved list has a create action. Paths default to the source note's directory. The sidebar has a new-note dialog. Creating a ghost refreshes the source note so the resolved link/backlink appears immediately.
- **Rename/move:** a native dialog changes the vault path and optionally rewrites inbound links; the result reports the rewritten note count and warnings.
- **Search:** ⌘K/Ctrl-K palette with debounced FTS search, grammar hint, sanitized `<mark>` snippets, arrow-key selection, Enter navigation, and live-result refresh on SSE.
- **Live:** one EventSource in the app shell. `note.created|moved|deleted` refresh the tree; external updates refresh the open note. Own writes are recognized by ETag, so two tabs still see each other's changes and stale saves still conflict.
- **Shared libs:** `api.ts` now covers create/update/move/search and carries problem details plus the 412 ETag; `diff.ts` is a capped LCS line diff; `live.svelte.ts` and `toast.svelte.ts` hold shared rune state; native `Modal`, create/rename dialogs, search palette, conflict panel, and toasts are hand-built on the existing tokens.

### Go and build

- `server/internal/webui` embeds `dist` with `go:embed all:dist`, serves assets with immutable caching, returns 404 for missing `_app` files, and falls back to `index.html` for client routes (`/notes`, `/n/...`, `/login`). Unknown `/api/*` paths remain JSON problem responses.
- `scripts/build-web.sh` builds the SPA and copies it into `server/internal/webui/dist`, the source for the embed. The built assets are checked in so `go build`/`go test` work without Node.
- Router tests now cover the SPA shell, embedded asset caching, missing assets, HEAD/POST, traversal, and authenticated unknown-API 404s.
- `notes.sh` now proves ghost creation resolves a link and adds a backlink, and rename rewrites the inbound link with `links_rewritten=1`.

## Verification actually run

- `cd web && npm run check` — 0 errors, 0 warnings.
- `cd web && npm run build` via `scripts/build-web.sh` — static build written and copied into the embed.
- `cd server && gofmt -l .`, `go vet ./...`, `go test ./...` — pass, including `server/internal/webui` tests.
- Fresh-vault live run of the built `lewnotes-core` binary on `127.0.0.1:18080`:
  - setup → create notes → resolved/ghost links → ghost creation and backlink → `428`/`412` → save → PATCH → tree/search → rename + rewrite (`links_rewritten=1`) → delete all.
  - `/`, `/notes`, `/login`, `/setup`, and `/n/inbox/Welcome.md` return the embedded SPA shell from Go.
  - An immutable `_app` asset returns 200 with `Cache-Control: immutable`; a missing `_app` asset returns 404.
  - Unknown API route without auth returns `401 application/problem+json`.
  - Log scan: 0 password/token matches and 0 server ERROR lines.

## Decisions, deviations, and limits

- **No browser automation:** Chrome is unavailable in this environment, so the editor, dialogs, palette, two-tab conflict, and 390px layout were typechecked/built and their API paths exercised, but the manual click-through script remains unrun.
- **Textarea, not CodeMirror:** matches the slice risk note; CodeMirror syntax/wikilink completion is a follow-up.
- **Client-side conflict diff:** the spec's read-only `diff_base` endpoint is not implemented. The UI fetches the current note and diffs locally; recorded in spec §10.9.
- **Generated clients:** the typed fetch wrapper was extended by hand. Generated TS/Go clients remain with S5, per spec §10.1.
- **Two tabs:** stale writes conflict through ETags. Own-save SSE echoes are filtered by the resulting ETag rather than by `origin=web`, so another tab's save is still noticed.
- **Wikilinks in code fences** are still rewritten by the pre-parse replacement. The known reader limitation remains for a future marked tokenizer extension.

## Manual acceptance still needed

1. Desktop: login → new note → edit/autosave → preview → `⌘K` search → ghost create → rename and confirm the rewritten toast/backlink.
2. Two tabs on one note: save in tab A, type in tab B, save B → conflict diff; test reload and overwrite.
3. Refresh with a dirty editor (warning), then with a clean one (no warning).
4. 390px: drawer, new/search dialogs, full-width editor, conflict panel, no horizontal page scroll.
