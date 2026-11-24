# lewnotes

A self-hostable, single-tenant notes server built on plain files: markdown notes, `todo.txt` tasks, and Excalidraw whiteboards, served by a Go + SQLite core with a Svelte web UI, a CLI, and a TUI.

The defining idea is **data-first**: the HTTP API is the product, and every interface is a peer client over it. Nothing about the web UI is special.

> **Status:** pre-alpha. The server owns a vault directory, indexes markdown notes into SQLite, assigns stable frontmatter IDs, and now requires authentication: a first-run owner setup, cookie sessions, and hashed `lnt.` device tokens. Every `/api/v1/*` route is private except health, setup, login, and auth status. Notes CRUD and search are next.

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
# API on :8080, with the vault in ./vault and the index in ./data
cd server && go run ./cmd/lewnotes-core -vault ../vault -data-dir ../data

# Web dev server on :5173, proxying /api to :8080
cd web && npm install && npm run dev
```

First boot has no owner. Create one with `POST /api/v1/auth/setup` (`{"password":"…","instance_name":"home"}`), then sign in with `POST /api/v1/auth/login`; both set the session and CSRF cookies. The end-to-end curl walkthrough lives in [`server/testdata/scripts/auth.sh`](server/testdata/scripts/auth.sh) and never prints the token it creates.

For plain-http local development set `LEWNOTES_COOKIE_SECURE=false` (cookies default to `Secure`).

```bash
curl -s localhost:8080/api/v1/health | jq
curl -s localhost:8080/api/v1/auth/status | jq

# The index is private: sign in first (or send a bearer token).
curl -s -c /tmp/lewnotes.jar -H 'Content-Type: application/json' \
  -d '{"password":"…"}' localhost:8080/api/v1/auth/login | jq
curl -s -b /tmp/lewnotes.jar localhost:8080/api/v1/index/status | jq

cd cli && go run ./cmd/lewnotes version --json
```

Notes are plain markdown; the server adds an `id:` ULID to frontmatter on first scan. Drop a `.md` file in the vault or `POST /api/v1/index/rescan` (body `{"full": true}`) to pick up changes made outside the server.

## Tests

```bash
cd server && go test ./...
cd cli && go test ./...
cd web && npm run check && npm run build
```

## License

TBD (see spec §18.16).
