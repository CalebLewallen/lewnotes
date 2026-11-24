# API rule

- Base path `/api/v1`. JSON in, JSON out, UTF-8, RFC 3339 UTC timestamps.
- Errors are `application/problem+json` with `type`, `title`, `status`, `detail`; never leak stack traces or file paths.
- Mutating a file requires `If-Match` with the current ETag. Missing → `428`, stale → `412` with the current ETag. Never overwrite silently.
- List endpoints paginate with `limit` + `cursor`. Unknown query parameters and unknown search filters are errors, not silently ignored.
- Add the OpenAPI entry and regenerate clients in the same change as the handler. CI fails on stale generated files.
- SSE events are named `<entity>.<action>`, carry `id` + JSON `data`, and include `origin` so a client can ignore its own echo.
- Every route is authenticated unless it is `/api/v1/health`, `/auth/status`, `/auth/setup` (pre-setup), or `/auth/login`.
