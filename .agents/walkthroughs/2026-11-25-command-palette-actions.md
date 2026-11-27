# Command palette actions

**Date:** 2026-11-25
**Status:** code complete; manual browser acceptance pending
**Depends on:** S4d

## What changed

- `SearchPalette.svelte` is now a command palette. Its root step keeps the
  debounced `/search` results and adds a filtered `Actions` group.
- Added `New note` and `New folder` commands. Both use a two-step flow:
  1. choose vault root or a folder;
  2. type the name and press `Enter`.
- Added `Rename`. It uses a target picker for notes and folders, then a
  prefilled, selected name input. Pressing `Enter` moves the item and reports
  link rewrites in a toast.
- Added `All notes` and `Sign out`, matching the sidebar footer actions.
- Create and rename reuse the existing API methods, path sanitization,
  `frontmatterForTitle`, tree refresh, route replacement, and toast behavior.
- Added a `Back` item to each intermediate step. `Esc` still closes the
  palette.
- Search now only runs in the root step. Requests are invalidated when the
  query, step, or dialog state changes so stale responses cannot overwrite an
  action flow.
- The app layout passes `refreshTree` and `logout` into the palette. Tree data
  comes from the existing vault-tree context.

No backend endpoint or schema change was needed. The sidebar actions already
map to existing API capabilities.

## Verification

- `cd web && npm run check`: 0 errors, 0 warnings.
- `bash scripts/build-web.sh`: pass; refreshed
  `server/internal/webui/dist`.
- Reviewed the create and rename paths against the sidebar implementation:
  same filename helpers, same frontmatter, same link-rewrite messages, same
  current-note route handling.

## What the next contributor should know

- `Enter` is handled by the selected `Command.Item`. In name steps, the
  create/rename item stays selectable even with an empty query so the first
  item does not move to `Back` while the user types. Submitting an empty name
  shows an inline error instead.
- The name input starts selected in rename mode. Typing replaces the old name;
  pressing `Enter` without changing it shows "Enter a different name."
- Commands and search results share the root query. Actions are ranked before
  search hits.
- The tree response is depth-limited to 6, so the palette cannot offer parents
  deeper than the sidebar can display.
- This environment has no browser. The manual keyboard pass is still pending:
  `Enter` selection, prefilled-name selection, `Back`, and visual layout.
