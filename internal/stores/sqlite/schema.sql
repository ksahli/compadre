-- The exchange, spread over three tables rather than dropped into one column
-- as a blob. A thread is instructions and an ordered list of turns, and a turn
-- is an ordered list of blocks, so that is what the tables are: the shape of
-- the core's own vocabulary, written down.
--
-- Every statement is IF NOT EXISTS, so applying this to a database that
-- already has it is not an error and there is no migration machinery to carry.
-- Tables are STRICT so that a column declared TEXT holds text: the usual
-- SQLite rules would take an integer written into one without complaint, and a
-- record that quietly changed shape on the way in is worse than a failed
-- write.

CREATE TABLE IF NOT EXISTS threads (
    id           INTEGER PRIMARY KEY,
    instructions TEXT NOT NULL,
    opened_at    TEXT NOT NULL
) STRICT;

-- ordinal is the turn's place in the exchange, and the UNIQUE over it is what
-- makes reading the thread back in the order it was said a matter of the
-- schema rather than of hoping rowids stay put.
CREATE TABLE IF NOT EXISTS messages (
    id      INTEGER PRIMARY KEY,
    thread  INTEGER NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    role    TEXT NOT NULL,
    UNIQUE (thread, ordinal)
) STRICT;

-- kind is the closed set of content shapes carried into the schema, and the
-- CHECK is what keeps it closed here as the unexported marker method keeps it
-- closed in Go. The columns after it are the fields of one shape each and are
-- null for the others; which ones are meant is settled by kind, so a read
-- never has to guess.
CREATE TABLE IF NOT EXISTS contents (
    id        INTEGER PRIMARY KEY,
    message   INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    ordinal   INTEGER NOT NULL,
    kind      TEXT NOT NULL CHECK (kind IN ('text', 'use', 'result', 'thinking', 'redacted')),
    text      TEXT,     -- kind = 'text': what was said
                        -- kind = 'thinking': the reasoning, often nothing at all
                        -- kind = 'redacted': the opaque blob, which is all there is
    signature TEXT,     -- kind = 'thinking': the model's proof it wrote the block
    call      TEXT,     -- kinds 'use' and 'result': the id that pairs them
    tool      TEXT,     -- kind = 'use': which tool
    arguments TEXT,     -- kind = 'use': the raw JSON, unparsed here as anywhere
    content   TEXT,     -- kind = 'result': what the model is shown
    failed    INTEGER,  -- kind = 'result': whether that is an explanation
    UNIQUE (message, ordinal)
) STRICT;
