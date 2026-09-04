-- The one thing applying schema.sql cannot do: widen a CHECK on a table that
-- is already there. CREATE TABLE IF NOT EXISTS leaves an existing contents
-- alone, so a database filed before thinking was a content shape would keep
-- refusing to write one, and every exchange in it would be unresumable the
-- moment the model reasoned.
--
-- SQLite has no ALTER for a CHECK, so this is the rebuild it does have:
-- the new table beside the old one, the turns copied across, the old one
-- dropped and the new one put in its place. It runs only when the old
-- definition is found, which is what makes it a no-op on every later open.
CREATE TABLE contents_rebuilt (
    id        INTEGER PRIMARY KEY,
    message   INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    ordinal   INTEGER NOT NULL,
    kind      TEXT NOT NULL CHECK (kind IN ('text', 'use', 'result', 'thinking', 'redacted')),
    text      TEXT,
    signature TEXT,
    call      TEXT,
    tool      TEXT,
    arguments TEXT,
    content   TEXT,
    failed    INTEGER,
    UNIQUE (message, ordinal)
) STRICT;

INSERT INTO contents_rebuilt
    (id, message, ordinal, kind, text, call, tool, arguments, content, failed)
SELECT id, message, ordinal, kind, text, call, tool, arguments, content, failed
  FROM contents;

DROP TABLE contents;

ALTER TABLE contents_rebuilt RENAME TO contents;
