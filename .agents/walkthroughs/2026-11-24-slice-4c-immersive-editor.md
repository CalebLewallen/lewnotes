# S4c — Immersive note surface and sidebar file manager

**Date:** 2026-11-24
**Status:** code complete; manual browser acceptance pending
**Depends on:** S4b

## What changed

- Replaced the textarea + Write/Preview toggle with a single CodeMirror 6 surface.
  Markdown remains the storage format. Headings, emphasis, inline code, quotes,
  lists, links, and images are decorated live; formatting marks appear only on
  the line holding the cursor. Wikilinks and markdown links open with
  `⌘`/`Ctrl`-click so a plain click always edits.
- Removed read/edit modes. Notes open editable, with save state in the top bar.
  Autosave, `If-Match`, conflict diff, save-before-navigation, and the remote
  change banner are preserved.
- Split frontmatter from the body. The editor only shows the body. The title is
  frontmatter `title`; tags are chips; a **Properties** popover edits other
  top-level frontmatter keys (text, number, boolean, or JSON values). `id` and
  location remain server-managed.
- New notes are created with frontmatter `title` and no duplicate body H1.
  Existing H1 bodies are left untouched; the H1 remains a display fallback until
  the title is edited.
- Committing a title (blur or Enter) saves first, then moves the note to a
  sanitized matching filename in the same directory and rewrites inbound links.
  Sidebar renames move the file directly.
- Replaced the global **New note** button with a per-folder `+` menu and inline
  create rows for notes and folders. Notes and folders also have inline rename
  rows behind a row menu. Paths are never edited on the note page.
- Added directory mutation APIs: `POST /api/v1/dirs` and
  `POST /api/v1/dirs/move`, with OpenAPI entries, service/API tests, and
  `tree.changed` SSE. Directory moves reindex contained notes and rewrite
  full-path wikilinks; basename links remain valid.
- Removed the old `marked`/preview renderer and the rename dialog. The embedded
  SPA is rebuilt with `scripts/build-web.sh`.
- Follow-up polish: the editor fills the remaining viewport and blank-area clicks
  place the cursor on the nearest line. GFM tables render when the cursor is
  outside them. Fenced code is syntax-highlighted for common languages, and the
  last code fence gets an escape line from ArrowDown or a click below it. Inline
  `#tag` text is styled as a tag. Typing `[[` opens note completion backed by
  the live sidebar tree.

## Verification

- `npm run check`: 0 errors, 0 warnings.
- `npm run build`: pass; CodeMirror is a large note-route chunk (~527 kB), noted
  as a possible follow-up code-split.
- Frontmatter/path helper tests passed under Node (`split`, `join`, title/tags,
  custom properties, ID repair, filename/path derivation).
- Decoration-builder test passed against a full sample document with cursors on
  headings, links, code, and plain text; empty documents produce no decorations.
- Backend directory service and API tests pass, including a folder move that
  rewrites an external full-path link and preserves a bare basename link.
- `gofmt`, `go vet`, and the full Go test suite pass.
- Embedded SPA rebuild and live API/SPA smoke pass; no secrets in logs.
- Table parser/decoration helper tests pass: a table renders when the cursor is
  outside it, stays as source while inside, and tags/wikilinks inside a rendered
  table do not add conflicting decorations. Code-fence escape cases pass,
  including shorter and longer fence markers.

## Limitations / follow-ups

- No browser in this environment, so the live marker behavior, popover, inline
  create/rename, two-tab conflict, and 390px layout still need a manual pass.
- The live layer does not render every markdown construct (footnotes, rich
  embeds). Note completion covers paths; alias and fragment completion are still
  deferred.
- The conflict diff remains client-side.
- The note route's CodeMirror chunk is large enough to warrant lazy loading if
  initial note navigation feels slow.
