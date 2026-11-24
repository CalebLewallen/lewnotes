# Secrets rule

- Never commit a real token, password hash, `.env`, vault file, or database. Examples use obvious placeholders.
- Logs must never contain tokens, passwords, cookie values, or full request bodies for auth routes. Log token IDs, never secrets.
- Device tokens are stored as `sha256(secret)` with a public prefix for display; plaintext is shown exactly once, at creation.
- Passwords use argon2id with per-user salt. Compare in constant time.
- The CLI stores credentials in `~/.config/lewnotes/config.toml` with mode `0600`; `LEWNOTES_TOKEN` overrides stored config.
- Error messages for auth failures are uniform; never reveal whether a password, token, or user exists.
- If a secret is exposed in a fixture, test log, or commit, rotate it; do not just delete the line.
