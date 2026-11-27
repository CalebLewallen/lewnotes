# lewnotes — web design system

**Status:** v0.3, slice S4d. Dark-first, keyboard-friendly, mobile-tolerant. UI primitives come from shadcn-svelte; custom components require a recorded exception.
**Spec:** [`lewnotes-spec.md`](lewnotes-spec.md) §12.

## Principles

1. **The note is the interface.** Chrome recedes: one sidebar, one reading column, no dashboards.
2. **Readable at arm's length.** 16px body, 1.75 line height, a measured column (max 48rem), restrained color.
3. **Density without noise.** File trees and lists use compact rows and hairline dividers instead of cards inside cards.
4. **Every state is named.** Loading, saving, unsaved, offline, unresolved, ambiguous, conflict, and error all have explicit copy.
5. **Dark first, not dark only.** Tokens are defined in OKLCH so a light theme is a token swap, not a rewrite.
6. **Touch is tolerated, not optimized.** Sidebar becomes a drawer under 768px; no hover-only actions.

## Tokens

Defined in `web/src/app.css` under `@theme`.

| Token | Value (OKLCH) | Use |
|---|---|---|
| `surface` | `0.16 0.006 285` | page background |
| `surface-raised` | `0.21 0.007 285` | sidebar, inputs, code blocks |
| `border-subtle` | `0.28 0.008 285` | hairlines, dividers |
| `muted` | `0.68 0.01 285` | metadata, paths, hints |
| text | `neutral-100` | primary copy |
| accent (links) | `0.78 0.11 235` | note links, focus affordances |
| ghost link | `0.7 0.09 70` | unresolved wikilink that creates a note |
| ambiguous link | `0.72 0.08 300` | link with more than one shortest match |
| danger | `rose-300` on `rose-950/40` | form, save, and conflict errors |

Typography: system sans for UI, system mono for paths, ids, diffs, and code. Type scale: 3xl page title, xl section, base body, sm controls, xs metadata, 10–11px labels with letter spacing for eyebrows.

Spacing: 4px base. Content padding 20px mobile / 32px desktop. Vertical rhythm follows the reading scale (16px between paragraphs, 32px before headings).

## Components

- **App shell** — shadcn `Sidebar.Provider` and `Sidebar.Root` (18rem) plus the reading column. The generated mobile sidebar sheet replaces the hand-built drawer and scrim. The live connection indicator stays in `Sidebar.Footer`.
- **Tree** — flat list derived from the nested `/tree` response inside `Sidebar.Menu`. The API orders files before folders in every directory, so notes sit directly under their parent and sibling folders follow. Folders collapse with a chevron, and a `Collapse all`/`Expand all` control sits above the tree. Notes and folders are draggable between folders; while dragging, folders highlight as drop targets and a `Move to vault root` zone appears. Folder actions use `DropdownMenu` for inline new note/folder creation and rename; notes navigate; attachments open in a new tab. New notes are created inside `inbox/`: the palette offers the inbox and its subfolders as parents, never the vault root. `attachments/` offers no note creation because it is upload-only (the server rejects notes there). Refreshed by SSE.
- **Note surface** — no read/edit split. The note opens as a borderless, always-editable document: title, tag chips, and a live body. There is no editable path field; the sidebar owns paths.
- **Title and attributes** — the large title is a shadcn `Textarea` styled as a heading, editing frontmatter `title`. Committing it (blur or Enter) saves first, then moves the note to a matching sanitized filename in the same directory and rewrites inbound links. Tags use `Badge`. **Properties** is a `Popover` with `Field`, `Input`, `NativeSelect`, `Checkbox`, and `Textarea`. `id` and location stay server-managed.
- **Live body** — one CodeMirror pane (`MarkdownEditor`, approved custom exception). The surface fills the remaining viewport; clicking its blank area places the cursor on the nearest line. Headings, emphasis, inline code, quotes, lists, links, images, and GFM tables render as you type; a table stays editable while the cursor is inside it. Fenced code is syntax-highlighted for common languages. Formatting marks appear only on the cursor's line. Markdown remains the storage format.
- **Inline tags** — `#tag` is styled as a tag; headings remain a `#` followed by a space.
- **Link completion** — typing `[[` opens note completion backed by the live sidebar tree and inserts a wikilink with its closing brackets.
- **Command palette** — `Command.Dialog` with debounced `/search`, typed results, `<mark>` snippets, keyboard selection, and result navigation. The root step also lists sidebar actions. New note/folder and rename use stepped `Command.*` lists: choose a parent or target, then type the name and press `Enter`; `Back` returns one step.
- **Dialogs** — shadcn `Dialog` for ghost-note creation and the command palette. Sidebar create/rename is inline. One primary action per dialog; focus enters the first field; Esc closes.
- **Conflict panel** — shadcn `Card` and `CodeBlock` diff lines (added/removed) for server-vs-yours, plus `Button` reload/overwrite actions. No automatic merge. The client-side `lineDiff` algorithm stays.
- **Backlinks / unresolved links** — sections below the body, separated by hairlines.
- **Buttons and fields** — `Button` variants (`default`, `outline`, `ghost`, `destructive`) and `Field` + `Input` + `Textarea` + `NativeSelect` + `Checkbox`. There are no `btn-*` or `.field` utility classes. The live body uses `.markdown-editor`.
- **Toasts** — Sonner `Toaster`, mounted once in the app layout.
- **Empty and error states** — `Empty` for empty collections, `Alert` for save/conflict/form errors, each with one action; never a blank pane.

## Keyboard

| Key | Action |
|---|---|
| `⌘K` (macOS) / `Alt-K` (Windows, Linux) | open/close the command palette |
| `/` | open the command palette when focus is not in a field |
| `⌘/Ctrl-click` | follow a link from the live body |
| `Enter` in the title | commit the title and sync the filename |
| `⌘S` / `Ctrl-S` | save immediately |
| `↑` / `↓`, `Enter` | move through and run palette commands or open results |
| `Esc` | close a dialog or the palette |
| `Tab` / `Shift+Tab` | traverse sidebar, filter, content links |
| `Enter` | follow focused link / submit form |

Global shortcuts are suppressed while a dialog is open. `g n` / `g t` navigation is intentionally deferred.

## Autosave and conflicts

- Autosave starts 900ms after the last body or attribute change; the note is always editable. The top line shows `Saving…`, `Unsaved changes`, or `Saved`.
- Title edits are saved like any attribute. The filename move runs only when the title is committed, so typing does not churn paths.
- Every write sends the ETag it started from as `If-Match`. A `412` never writes; the conflict panel fetches the current note and shows a client-side line diff.
- **Reload server version** discards local text after confirmation. **Overwrite with mine** deliberately writes against the server's current ETag; if the note changes again, the panel refreshes.
- Dirty navigation is held: the note saves first, then the destination opens. Browser refresh/close warns while dirty.
- External SSE changes reload a clean note in place. If the editor is dirty, a banner offers Review difference or Reload.
- A ghost link opens a create dialog prefilled from the source note's directory. Creating it refreshes the current note so the resolved link and backlink appear immediately.

## Layout and breakpoints

- **≥768px:** persistent sidebar, borderless note column centered in the remaining width (wider than the list view).
- **<768px:** top bar with menu/search; drawer overlays at 85vw max; content full width with 20px gutters; live editor, popover, and diff stay inside their containers.
- Target check: 390px wide with no horizontal page scroll. Code blocks, tables, and diffs scroll inside their own containers.

## Deliberate exclusions

- The live layer does not render every construct: footnotes and rich embeds may still read as source.
- The conflict diff is computed in the browser from two note revisions. A server-side `diff_base` diff endpoint remains deferred.
- The palette is the search and command surface; a full `/search` page is not built.
- Attachments, todos, whiteboards, settings/tokens, and a persistent mobile capture button remain later slices.
- The SPA is embedded, but CSP/security-header hardening is S10.
