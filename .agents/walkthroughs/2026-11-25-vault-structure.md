# Walkthrough: vault structure — notes at root, attachments upload-only

**Date:** 2026-11-25 · **Status:** complete (uncommitted)

## What changed

- New notes start at the **vault root** instead of `inbox/`. The ghost-note dialog's fallback path and placeholder are now `Untitled.md`; the palette already defaulted to `Vault root`.
- `inbox/` and `attachments/` are still scaffolded on first start. `inbox/` remains an available convention, not a required parent.
- `attachments/` is reserved for uploaded files:
  - The sidebar hides **New note** on the attachments folder.
  - The command palette omits attachments (and anything below it) from the parent list when creating a note. Folder creation there is still allowed so uploads can be organized.
  - The server rejects note creation, note moves, and directory moves whose destination is inside the configured attachments folder. This covers API, CLI, and any other client, not just the web UI.
- Wiring: `notes.Options.Attachments` receives `vault.attachments` from config; the web uses `ATTACHMENTS_DIR` / `isAttachmentsPath` in `paths.ts`.
- Spec decision #17 and §7.1, plus `DESIGN.md`, describe the new behavior.

## Verified

- `cd server && go test ./...` — all packages pass, including the new `TestAttachmentsRejectNotes` (create, subfolder create, note move, dir move).
- `cd server && go build ./...` — clean; `gofmt` reports nothing.
- `cd web && npm run check` — 0 errors, 0 warnings (only the pre-existing `config.alias` deprecation notices).
- `bash scripts/build-web.sh` — refreshed the embedded SPA.
- Rebuilt and restarted the dev API on `:8080`; `/api/v1/health` returns 200 directly and through the Vite proxy.

## Update 2026-11-26

- Reversed the default: the **New note** command now starts in `inbox/`, not the
  vault root. The command palette leads with `inbox` (preselected); the CLI
  `note new` writes to `inbox/` unless `--folder` or `--path` says otherwise.

## Update 2026-11-26 (later)

- Made the inbox the actual root for notes instead of only the default. The
  palette's **New note** flow now offers `Inbox` and folders inside it, never
  `Vault root` or sibling top-level folders. CLI `note new` writes inside
  `inbox/`: a `--folder` value is relative to the inbox, and `--path` must start
  with `inbox/` (creating outside it exits 2). Subfolders inside `inbox/` remain
  free-form. Per-folder **New note**, ghost-link creation, and moves are
  unchanged.

## Note for later

The frontend special-cases the literal folder name `attachments`; if `vault.attachments` is renamed in config, the server guard still protects the correct folder but the UI hint would not follow. Exposing the configured name (for example in `/api/v1/meta`) would fix that when the attachments slice lands.
