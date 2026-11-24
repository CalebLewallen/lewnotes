# AGENTS.md

Guide for anyone — human or AI — working in this repository. Read this first, then the spec and the active plan.

## What this is

lewnotes is a self-hostable, single-tenant notes server. Files are the source of truth; SQLite is a rebuildable index plus server state. The HTTP API is the product; the web UI, CLI, and TUI are peers over it.

- **Spec:** `lewnotes-spec.md` (formats, API surface, auth, milestones, open questions)
- **Build plan:** `.agents/plans/2026-11-24-build-slices.md` (what to do next)
- **Design system:** `DESIGN.md` (required before web UI work)
- **Rules:** `.agents/rules/*.md` (binding; read the ones your change touches)

## Layout

```
server/   Go module: lewnotes-core (HTTP API, vault, index, auth)
cli/      Go module: lewnotes binary (CLI + TUI) and client/ HTTP package
web/      SvelteKit 5 SPA, Tailwind 4, shadcn-svelte
docker/   Dockerfile + compose
.agents/  skills/, rules/, plans/, walkthroughs/
```

Two Go modules under one root `go.work`. `cli/client` must not import `server/internal/...`.

## Commands

```bash
# server
cd server && go run ./cmd/lewnotes-core        # :8080
cd server && go test ./... && go vet ./...

# cli
cd cli && go run ./cmd/lewnotes version --json
cd cli && go test ./...

# web
cd web && npm run dev                          # :5173, proxies /api → :8080
cd web && npm run check && npm run build

# docker
docker compose -f docker/docker-compose.yml up --build
```

## Invariants

1. **Files are truth.** Never store note/todo/drawing content only in SQLite. Deleting the DB must lose only sessions, tokens, and prefs.
2. **The server is the only writer.** No filesystem watchers, no merge-on-drift. Serialize writes per path, write atomically (temp + fsync + rename).
3. **Byte fidelity.** Frontmatter and `todo.txt` lines are edited surgically. Untouched bytes must survive every read-modify-write. Golden round-trip tests protect this.
4. **IDs live in frontmatter** (`id:` ULID). Paths are mutable; IDs are not.
5. **API-first.** Every UI capability gets an endpoint and an OpenAPI entry in the same change. No UI-only state that matters.
6. **Path safety.** All vault paths are relative and cleaned; symlinks may not escape the vault root.
7. **Mutations are conditional.** File writes require `If-Match`; stale writes return `412`. Never silently overwrite.
8. **No secrets in logs, fixtures, or the repo.** Tokens are hashed at rest and printed at most once.

## Workflow

- **Plan before multi-step work.** Add or update a file in `.agents/plans/` (`YYYY-MM-DD-<slug>.md`) before writing code that spans more than one slice of behavior. Update the plan's status table as you go; don't leave it stale.
- **Walkthrough after.** When a slice is finished, write `.agents/walkthroughs/YYYY-MM-DD-<slug>.md`: what changed, what you verified, what you learned, what's next.
- **One slice at a time.** The plan defines "done when" checks. Run them; do not mark a slice done on a compile alone.
- **Decisions.** If you must deviate from the spec, record the change in the spec's open-questions/decision log in the same change.

## Skills available in `.agents/skills/`

| Skill | Load when |
|---|---|
| `svelte-runes` | Writing any Svelte 5 component (`$state`, `$props`, `$derived`, `$effect`) |
| `svelte-components` | Building component APIs, snippets, Bits UI/Ark patterns |
| `svelte-styling` | Scoped CSS, CSS variables, `:global`, Tailwind edge cases |
| `sveltekit-structure` | Routing, layouts, `+page`/`+layout` files, project structure |
| `sveltekit-data-flow` | Load functions, form actions, error handling |
| `shadcn` | shadcn registries, `components.json`, design-system composition — **React examples do not apply**, we use `shadcn-svelte` |
| `golang-code-style` | Writing/reviewing Go |
| `golang-testing` | Table tests, golden files, `httptest`, fuzzing |
| `golang-error-handling` | Wrapping, sentinel errors, error surfaces |
| `golang-cli` | Cobra/flags/exit codes/config layering in `cli/` |
| `golang-concurrency` | Goroutines, mutexes, SSE hub, scanners |
| `golang-security` | Auth, path handling, token storage, input limits |

Install more with `npx skills add <owner/repo> --skill <name> -y` (project-local). Review third-party skills before relying on them; never edit their contents, add a project rule or a local skill instead.

## Conventions

- Go: standard library first; `log/slog`; errors wrapped with context; `internal/` for non-public packages.
- API: `/api/v1`, `application/problem+json` errors, RFC 3339 timestamps, cursor pagination.
- Web: Svelte 5 runes only, TypeScript, hand-built primitives on the dark-first tokens in `DESIGN.md` (shadcn-svelte optional, not required).
- Tests: table-driven; fixture vaults under `testdata/`; no network in unit tests.
- Commits: imperative subject, body explains *why*.
