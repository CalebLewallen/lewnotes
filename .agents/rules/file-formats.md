# File format rule

- Never reformat content you did not mean to change. A read-modify-write must be byte-identical outside the intended edit.
- Frontmatter: preserve key order, quoting, and comments. Insert `id:` as the last line of an existing block; create a minimal block only when absent.
- `todo.txt` lines: bare lines, no bullets or checkboxes. Preserve unknown `key:value` pairs, spacing, and trailing whitespace.
- Every parser change needs a golden round-trip test (`parse → serialize` byte-identical) plus at least one odd-input case (unicode, tabs, empty description).
- Wikilinks: case-insensitive, shortest-unique-suffix resolution, extension stripped. Unresolved links are a valid indexed state, not an error.
- Renames rewrite inbound links only when safe: full-path links always, basename links only when uniqueness would break. Preserve aliases and fragments.
- Notes are `.md`; drawings are `.excalidraw` JSON; everything else is an attachment and is never parsed as content.
