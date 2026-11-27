# Walkthrough: sidebar tree ordering, folding, and drag-and-drop

**Date:** 2026-11-25 · **Status:** complete (uncommitted)

## What changed

- **Ordering:** `Tree` now returns files (notes, images, boards) before folders in every directory, each group sorted case-insensitively by name. A folder header no longer appears above the notes that belong to its parent.
- **Collapsible folders:** folder headers have a chevron and toggle their descendants. A `Collapse all` / `Expand all` control sits above the tree. Collapsed state is in memory; the default is expanded.
- **Drag and drop:**
  - Drag a note onto a folder to move it there; drag a folder onto another folder to move it, with contained notes reindexed and full-path links rewritten by the existing move endpoints.
  - Dropping onto `attachments/` is blocked for notes and for folders from outside it, matching the server guard.
  - Dropping a folder into itself or one of its descendants is blocked. Dropping into the current parent is a no-op rather than a redundant request.
  - While dragging, eligible folder rows highlight and a `Move to vault root` target appears when the dragged item is nested. The open note follows its new path after a move.
- If a collapsed folder is moved, its collapsed descendant keys are remapped to the new prefix.

## Verified

- `cd server && go test ./...` — includes the new `TestTreeSortsFilesBeforeDirectories`; all packages pass.
- `cd web && npm run check` — 0 errors, 0 warnings.
- `bash scripts/build-web.sh` — refreshed the embedded SPA.

## Limits

- Drag-and-drop uses native HTML5 drag events (no new dependency). It is pointer-only; there is no keyboard move command beyond the existing Rename flow.
- Collapsed state is not persisted across reloads.
