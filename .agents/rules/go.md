# Go rule

- Standard library first. Add a dependency only when it removes more code than it adds, and pin it in `go.mod`.
- `log/slog` for logs; structured key/value pairs; no `fmt.Println` in library code.
- Wrap errors with context (`fmt.Errorf("...: %w", err)`); sentinel errors for caller-visible states; never swallow.
- Accept `context.Context` first and honor cancellation in anything that touches disk, network, or SQLite.
- Keep packages under `internal/` unless they are a deliberate public contract (`cli/client` is).
- No mutable package-level state except build-time version variables and the default logger.
- Tests: table-driven, `t.TempDir()` fixtures, `httptest` for handlers, `-race` in CI. Golden files live in `testdata/`.
- SQLite writes go through one serialized writer; do not open ad-hoc connections from handlers.
