# Plan: command palette actions

**Status:** code complete; manual browser acceptance pending · **Started:** 2026-11-25 · **Depends on:** S4d

## Goal

Make the `Command.Dialog` palette the app's command surface as well as its
search surface. Every action reachable from the sidebar should be executable
from the palette without leaving the keyboard.

The palette should keep searching while adding commands. Multi-step actions
should ask one question at a time and advance on `Enter`, rather than exposing
several fields.

## Scope

- Root step: show sidebar actions and debounced `/search` results together,
  filtered by the same query where appropriate.
- New note: choose a parent (vault root or an existing folder), then type a
  title and press `Enter`.
- New folder: choose a parent, then type a folder name and press `Enter`.
- Rename note/folder: choose a note or folder, then type the new name and press
  `Enter`.
- All notes: navigate to `/notes`.
- Sign out: run the same logout path as the sidebar footer.
- Reuse the existing `/notes`, `/dirs`, and `/dirs/move` endpoints plus the
  existing path/frontmatter helpers, tree context, SSE refresh, and route
  updates. No API or schema change is needed.

## Decisions

- `Command.Item` selection is the `Enter` mechanism for each step. The name
  step renders a selectable create/rename item first, so typing followed by
  `Enter` runs it even when no list filtering is involved.
- Every step is a `Command.*` list. No new custom primitive is introduced.
- A `Back` item returns one step. `Esc` retains the standard dialog close.
- Search requests are invalidated when the query, step, or open state changes so
  a late response cannot leak results into an action step.
- `All notes` and `Sign out` are included because they are sidebar actions.
  Viewport-only controls such as the mobile sidebar trigger are not commands.

## Status

| Step | Status |
|---|---|
| Action catalog and combined root search/commands | ✅ code complete |
| Stepped new note/new folder flow | ✅ code complete |
| Stepped rename target/name flow | ✅ code complete |
| All notes and sign out commands | ✅ code complete |
| Documentation and embedded build | ✅ complete |
| Browser keyboard pass | ⬜ pending (no browser in this environment) |

## Done when

- `cd web && npm run check` passes with no errors or warnings.
- `bash scripts/build-web.sh` passes and refreshes the embedded SPA.
- Manual browser script passes:
  - open the palette with the OS shortcut and `/`;
  - type `new note`, choose a folder, type a title, press `Enter`, and land on
    the new note;
  - create a folder and see it in the tree after refresh;
  - rename a note and a folder through the palette;
  - run `All notes` and `Sign out`;
  - confirm search results still navigate and that `Esc` closes the palette.

## Verified 2026-11-25

- `cd web && npm run check`: 0 errors, 0 warnings.
- `bash scripts/build-web.sh`: pass; SPA copied to
  `server/internal/webui/dist`.
- Source review: action flows reuse the sidebar's path sanitization,
  frontmatter creation, link-rewrite reporting, tree refresh, and current-route
  update behavior.

## Manual pass items

The environment has no browser, so these remain unverified: actual `Enter`
selection in Bits UI, focus/selection of the prefilled rename name, the `Back`
item flow, and visual layout of the action lists.
