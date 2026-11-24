# lewnotes — web design system

**Status:** v0.1, slice S4a. Dark-first, keyboard-friendly, mobile-tolerant.
**Spec:** [`lewnotes-spec.md`](lewnotes-spec.md) §12.

## Principles

1. **The note is the interface.** Chrome recedes: one sidebar, one reading column, no dashboards.
2. **Readable at arm's length.** 16px body, 1.75 line height, a measured column (max 48rem), restrained color.
3. **Density without noise.** File trees and lists use compact rows and hairline dividers instead of cards inside cards.
4. **Every state is named.** Loading, empty, offline, unresolved link, conflict, and error all have explicit copy.
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
| ghost link | `0.7 0.09 70` | unresolved wikilink |
| danger | `rose-300` on `rose-950/40` | form and request errors |

Typography: system sans for UI, system mono for paths, ids, line numbers, and code. Type scale: 3xl page title, xl section, base body, sm controls, xs metadata, 10–11px labels with letter spacing for eyebrows.

Spacing: 4px base. Content padding 20px mobile / 32px desktop. Vertical rhythm follows the reading scale (16px between paragraphs, 32px before headings).

## Components

- **App shell** — sticky/static sidebar (18rem) + reading column. Mobile top bar, off-canvas drawer, scrim.
- **Tree** — flat list derived from the nested `/tree` response. Directory labels are non-interactive; notes navigate; attachments open in a new tab. Client-side filter.
- **Note header** — path, title, updated date, tag pills, link/backlink counts.
- **Note body** — sanitized markdown; wikilinks use the API's resolution (`exact`/`suffix` → route, otherwise a dashed ghost span).
- **Backlinks / unresolved links** — sections below the body, separated by hairlines.
- **Buttons** — `btn-primary` (one per screen), `btn-ghost` (shell actions). Inputs use `.field`.
- **Empty and error states** — plain prose plus one action; never a blank pane.

## Keyboard

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | traverse sidebar, filter, content links |
| `Enter` | follow focused link / submit form |
| `Esc` (later) | close drawer or palette |

The command palette (`⌘K`), editor shortcuts, and save/conflict chords arrive with the editor slice. This slice has no custom key handling, so browser and screen-reader behavior remain intact.

## Layout and breakpoints

- **≥768px:** persistent sidebar, reading column centered in the remaining width.
- **<768px:** top bar with menu; drawer overlays at 85vw max; content full width with 20px gutters.
- Target check: 390px wide with no horizontal page scroll. Code blocks and tables scroll inside their own containers.

## Deliberate exclusions in S4a

Read-only browsing only: no editor, autosave, command palette, backlink creation, ghost-link creation, rename dialog, conflict diff, SSE refresh, or SPA embedding in the Go binary. Those are S4b, so this slice can be steered after a real login.
