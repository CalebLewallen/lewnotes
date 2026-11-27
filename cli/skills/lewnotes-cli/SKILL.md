---
name: lewnotes-cli
description: "Operate a lewnotes vault through the lewnotes CLI: search, read, create, append, move, and delete notes safely with JSON output and conflict checks."
---

# lewnotes CLI

Use `lewnotes` when the task is to read or change notes in a lewnotes vault from
a terminal or an agent. The server owns the vault; never edit its files directly.

## Credentials

Prefer a device token from the environment:

```bash
export LEWNOTES_SERVER="https://notes.example.com"
export LEWNOTES_TOKEN="lnt...."
```

If the host is already configured, `lewnotes whoami --json` should succeed.
Never print the token, put it in a note, or commit it. If authentication fails,
create a new token at `/settings/tokens` on the server; do not try passwords.
For a human on an interactive machine, `lewnotes login <server>` is the supported
way to obtain one: it opens the web app for approval and stores the credential.
Agents should keep using `LEWNOTES_TOKEN`, since they cannot approve a device.
The token embeds the server origin, so `LEWNOTES_TOKEN` alone is enough when no
config file exists.

## Output contract

Pass `--json` on every command. The envelope is stable:

```json
{"ok": true, "data": {}, "warnings": []}
```

On failure `ok` is false and `error` carries `{code, message, status?}`. Exit
codes: `0` ok, `1` failure, `2` usage, `3` ambiguous reference, `4` auth,
`5` conflict, `6` unreachable. Use `--json=lines` for lists.

Discover the full command tree with `lewnotes help --json` rather than guessing.

## Referencing notes

Commands accept a `<ref>`: exact ULID, exact path, unique path suffix, or unique
title. Ambiguous refs exit `3` and list candidates; retry with a full path.
Always quote titles containing spaces.

## Safe editing

- Read before writing: `lewnotes note show <ref> --json`.
- Prefer `lewnotes note append <ref> --stdin` over rewriting a whole note.
- `note edit` uses `$VISUAL`/`$EDITOR` and saves with the ETag it read.
- A conflict exits `5`. The note changed underneath you: re-read it and redo the
  edit. Never blind-overwrite with `--force`-style flags; there are none.
- `note mv` rewrites inbound wikilinks by default. Leave that on unless told not
  to.
- Deletes are soft (`rm` moves to `.lewnotes/trash`) and require `--yes` when
  stdin is not a terminal. Do not bulk-delete; list first.
- `attachments/` is for uploaded files. Notes cannot be created or moved there.
- `note new` always creates notes inside `inbox/`; the vault root is not a note
  destination. `--folder` takes a folder inside the inbox (`--folder projects`
  creates `inbox/projects/<title>.md`), and `--path` must start with `inbox/`.

## Examples

```bash
lewnotes search "tag:project is:unresolved" --json
lewnotes note show "Kitchen plan" --json
printf 'Follow up Friday\n' | lewnotes note append "Kitchen plan" --stdin --json
lewnotes note new "Trip ideas" --folder projects --body "# Trip ideas" --json
lewnotes note mv "Trip ideas" projects/2027-trip.md --json
```

If a write fails, surface the error and the exit code instead of retrying the
same stale write.
