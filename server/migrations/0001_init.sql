-- 0001: content index. SQLite is a rebuildable index over the vault; deleting
-- this database must never lose note content.

CREATE TABLE notes (
    id            TEXT PRIMARY KEY,
    path          TEXT NOT NULL UNIQUE,
    title         TEXT NOT NULL DEFAULT '',
    content_hash  TEXT NOT NULL,
    size          INTEGER NOT NULL,
    mtime         INTEGER NOT NULL,
    word_count    INTEGER NOT NULL DEFAULT 0,
    excerpt       TEXT NOT NULL DEFAULT '',
    frontmatter   TEXT NOT NULL DEFAULT '{}',
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE links (
    src_id      TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    dst_id      TEXT REFERENCES notes(id) ON DELETE SET NULL,
    dst_raw     TEXT NOT NULL,
    alias       TEXT NOT NULL DEFAULT '',
    fragment    TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL DEFAULT 'link',
    resolution  TEXT NOT NULL DEFAULT 'unresolved',
    PRIMARY KEY (src_id, dst_raw, kind, fragment)
);

CREATE TABLE tags (
    note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (note_id, tag)
);
CREATE INDEX tags_tag ON tags(tag);

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Standalone FTS5 table maintained by the indexer (not external-content, so
-- deletes and partial updates stay simple).
CREATE VIRTUAL TABLE notes_fts USING fts5(
    note_id UNINDEXED,
    title,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);
