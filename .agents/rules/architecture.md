# Architecture rule

- The HTTP API is the contract. Any capability added to the web UI, CLI, or TUI must exist as an endpoint first.
- `server/` owns the vault and the SQLite index. `cli/client` talks HTTP only and must never import `server/internal/...`.
- SQLite is disposable: a rebuild from files must reproduce the same index. Only sessions, tokens, and preferences may be lost.
- The server is the sole writer of vault files. Do not add a filesystem watcher or a merge-on-external-change path; log drift instead.
- One tenant per instance. No roles, no per-note permissions, no multi-user assumptions in schema or API.
- Serialize writes per path and write atomically; never truncate a file in place.
