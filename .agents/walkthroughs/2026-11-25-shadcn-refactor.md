# S4d — Mandatory shadcn-svelte refactor

**Date:** 2026-11-25
**Status:** code complete; manual browser acceptance pending
**Depends on:** S4c

## What changed

- Catalogued every custom UI surface and mapped it to a shadcn-svelte registry
  component. shadcn-svelte is now the required starting point for web UI.
- Installed the `vega` preset with the `neutral` base and mapped the existing
  `DESIGN.md` dark tokens onto the shadcn semantic variables.
- Replaced the custom modal with `Dialog` and rebuilt note creation with
  `Dialog`, `Field`, `Input`, `Textarea`, and `Button`.
- Rebuilt the search palette with `Command.Dialog` and `Command.*`. The existing
  debounced `/search` call, typed results, snippets, and navigation remain;
  keyboard selection and Escape now come from Command.
- Replaced the custom toast store and toast renderer with Sonner. A small
  `toast.ts` adapter keeps existing call sites unchanged.
- Rebuilt the properties panel with `Popover`, `NativeSelect`, `Checkbox`,
  `Input`, `Textarea`, and `Button`. The YAML value conversion and protected
  `id`/`title`/`tags` checks are unchanged.
- Rebuilt the conflict panel with `Card`, `CodeBlock` added/removed lines, and
  `Button` actions. The client-side `lineDiff` algorithm is unchanged.
- Replaced the hand-built app sidebar, mobile drawer, scrim, and outside-click
  menus with `Sidebar.Provider`, `Sidebar.Root`, the generated mobile sheet,
  and `DropdownMenu`. A small `SidebarAutoClose` component closes the mobile
  sheet after navigation.
- Replaced page-level buttons, inputs, tags, alerts, note-list rows, empty
  states, auth fields, and the error action with `Button`, `Input`, `Badge`,
  `Alert`, `Item`, `Empty`, `Field`, and `Kbd`.
- The note title now uses a shadcn `Textarea` styled as the borderless heading.
- Removed `Modal.svelte`, `Toasts.svelte`, `toast.svelte.ts`, and the legacy
  `.field`, `.btn-*`, `.tree-link`, and `.diff-*` CSS rules.
- Added the `$lib` alias to the SvelteKit plugin config so the generated
  shadcn components resolve under SvelteKit 3.

## Approved custom areas

- `MarkdownEditor.svelte` and its CodeMirror table/image widgets remain custom:
  shadcn has no live markdown editor with rendered tables, marker hiding, code
  fences, and wikilink completion.
- No other custom primitive was retained. The catalog and exceptions are in
  `.agents/plans/2026-11-25-shadcn-refactor.md`.

## Verification

- `npm run check`: 0 errors, 0 warnings.
- `npm run build`: pass.
- `bash scripts/build-web.sh`: pass; SPA copied to `server/internal/webui/dist`.
- Source scan: no `btn-*`, `.field`, `tree-link`, or diff-only class remains.
- Source scan: no raw `<button>`, `<input>`, `<select>`, or `<textarea>` remains
  in route components.

## Limitations / follow-ups

- No browser is available in this environment, so the manual pass is still
  pending: dialog focus, palette keyboard flow, properties popover, mobile
  sidebar sheet, dropdown actions, toasts, and 390px layout.
- SvelteKit 3 warns that `config.alias` is deprecated. The generated shadcn
  components still import `$lib`, so the alias is required for now. Once the
  registry output or the project moves to `#lib`, remove the alias and the
  warning. This is a warning, not a build failure.
- `config.alias` is the only new build warning; the existing CodeMirror
  dynamic-import notice is unchanged.
