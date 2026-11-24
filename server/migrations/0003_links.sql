-- 0003: wikilink index detail. Links are rebuilt from note bodies; the line
-- number drives backlink display and candidates records ambiguous targets.

ALTER TABLE links ADD COLUMN line INTEGER NOT NULL DEFAULT 0;
ALTER TABLE links ADD COLUMN candidates TEXT NOT NULL DEFAULT '[]';

CREATE INDEX links_dst_id ON links(dst_id);
CREATE INDEX links_resolution ON links(resolution);
CREATE INDEX links_kind ON links(kind);
