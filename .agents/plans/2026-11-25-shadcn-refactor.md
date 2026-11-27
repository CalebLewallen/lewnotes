# Plan: shadcn-svelte component refactor

**Status:** code complete; manual browser acceptance pending · **Started:** 2026-11-25 · **Depends on:** S4c

## Decision

shadcn-svelte is required for web UI work. It is the starting point for every
component. A custom component is allowed only when the registry has no close
equivalent, and that exception must be recorded here.

Initialize with the standard `vega` preset, the `neutral` base color, and the
existing `DESIGN.md` dark tokens mapped onto shadcn's semantic CSS variables.
Do not adopt a second visual language.

## Catalog and replacement map

| Existing custom UI | shadcn-svelte replacement | Status |
|---|---|---|
| `Modal.svelte` | `Dialog` | Replace and remove wrapper |
| `SearchPalette.svelte` modal shell | `Command.Dialog` + `Command.*` | Replace |
| `toast.svelte.ts` + `Toasts.svelte` | `Sonner` / `Toaster` | Replace |
| `CreateNoteDialog.svelte` shell | `Dialog` + `Field` + `Input` + `Textarea` + `Button` | Replace |
| `NoteProperties.svelte` popover shell | `Popover` + `Field` + `Input` + `NativeSelect` + `Checkbox` + `Textarea` + `Button` | Replace |
| `ConflictDiff.svelte` panel | `Card` + `CodeBlock` diff lines + `Button` | Replace; keep `lineDiff` |
| App shell desktop sidebar | `Sidebar.Provider` + `Sidebar.Root` + `Sidebar.*` | Replace |
| App shell mobile drawer | `Sidebar` mobile sheet | Replace |
| Sidebar add/row menus | `DropdownMenu` | Replace |
| Buttons, inputs, textareas, labels | `Button` + `Input` + `Textarea` + `Field` | Replace |
| Tags, status alerts, empty states | `Badge` + `Alert` + `Empty` | Replace |
| Notes list | `Item` + `Empty` | Replace |
| Shortcut hint | `Kbd` | Replace |
| `MarkdownEditor.svelte` | No shadcn-svelte equivalent | **Keep custom (approved)** |
| `TableWidget` and `ImageWidget` inside CodeMirror | Not Svelte UI primitives | **Keep custom (approved)** |

## Approved exceptions and tradeoffs

- **CodeMirror live editor:** shadcn has no live markdown/code editor. Replacing
  it with `Textarea` would remove rendered tables, inline tags, fenced-code
  behavior, and wikilink completion. Approved: keep CodeMirror and use shadcn
  for every surrounding control.
- **Conflict diff:** shadcn `CodeBlock` supports added/removed lines. Approved:
  use `CodeBlock` for the diff body and `Card`/`Button` for the panel. The
  client-side `lineDiff` algorithm stays.
- **Editor-rendered tables/images:** these are CodeMirror widgets, not app UI.
  Approved: keep them custom.

## Steps

1. Initialize shadcn-svelte and add the registry components in the map.
2. Map shadcn semantic variables to the `DESIGN.md` dark tokens; preserve the
   existing CodeMirror and rendered-markdown styles.
3. Replace dialogs, palette, toasts, properties, conflict panel, forms, alerts,
   badges, list items, and the app shell.
4. Remove the legacy `btn-*`, `field`, `tree-link`, and diff-only CSS.
5. Update `AGENTS.md`, `DESIGN.md`, and the active build plan so shadcn-svelte
   is no longer described as optional.
6. Verify `npm run check`, the production/embedded build, and focused browser
   behavior where available.

## Verified 2026-11-25

- `npm run check`: 0 errors, 0 warnings.
- `npm run build`: pass.
- `bash scripts/build-web.sh`: pass; SPA copied to `server/internal/webui/dist`.
- No `btn-*`, `field`, `tree-link`, or diff-only class remains in `web/src`.
- No raw `<button>`, `<input>`, `<select>`, or `<textarea>` remains in routes;
  the only custom interactive surface left is the approved CodeMirror editor.
- Non-fatal build warning: SvelteKit 3 deprecates `config.alias`. The generated
  shadcn components import `$lib`, so the alias is still required until the
  registry output or the project moves to `#lib`.

## Done when

- `web/components.json` and `web/src/lib/components/ui` exist and the registry
  components are imported by the app.
- No legacy custom primitive remains for a component with a shadcn equivalent.
- The two approved custom areas are documented and isolated.
- `npm run check`, `npm run build`, and `bash scripts/build-web.sh` pass.
- The note page, palette, dialogs, properties popover, conflict panel, sidebar,
  and auth screens still behave as before.
