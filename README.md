# lewnotes

A self-hostable, single-tenant notes server built on plain files: markdown notes, `todo.txt` tasks, and Excalidraw whiteboards, served by a Go + SQLite core with a Svelte web UI, a CLI, and a TUI.

The defining idea is **data-first**: the HTTP API is the product, and every interface is a peer client over it. Nothing about the web UI is special.

> **Status:** pre-alpha scaffolding. The server currently exposes only `GET /api/v1/health`. See the plan for what lands next.

## Documents

| File | What it is |
|---|---|
| [`lewnotes-spec.md`](lewnotes-spec.md) | Full product and technical spec (formats, API, auth, UI, milestones) |
| [`.agents/plans/2026-11-24-build-slices.md`](.agents/plans/2026-11-24-build-slices.md) | Slice-by-slice build plan and current progress |
| [`AGENTS.md`](AGENTS.md) | Repo guide for human and AI contributors |
| `.agents/rules/` | Short, binding project rules |

## Layout

```
server/   lewnotes-core: Go HTTP API, vault scanning, SQLite index
cli/      lewnotes: CLI + TUI, plus the shared HTTP client
web/      SvelteKit 5 SPA (Tailwind 4, shadcn-svelte)
docker/   Dockerfile and compose file
```

## Quickstart (dev)

```bash
# API on :8080
cd server && go run ./cmd/lewnotes-core

# Web dev server on :5173, proxying /api to :8080
cd web && npm install && npm run dev
```

```bash
curl -s localhost:8080/api/v1/health | jq
cd cli && go run ./cmd/lewnotes version --json
```

## Tests

```bash
cd server && go test ./...
cd cli && go test ./...
cd web && npm run check && npm run build
```

## License

TBD (see spec §18.16).
