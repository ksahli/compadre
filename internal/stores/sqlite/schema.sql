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
--
-- The two counts are what the turn cost, and they are null together: null is
-- nobody counted this turn, which is what every turn said to the model rather
-- than by it looks like, and is a different fact from a turn counted at zero.
CREATE TABLE IF NOT EXISTS messages (
    id            INTEGER PRIMARY KEY,
    thread        INTEGER NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    ordinal       INTEGER NOT NULL,
    role          TEXT NOT NULL,
    input_tokens  INTEGER,  -- tokens the turn was read from, null if uncounted
    output_tokens INTEGER,  -- tokens the turn was written in, null if uncounted
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
    kind      TEXT NOT NULL CHECK (kind IN ('text', 'use', 'result')),
    text      TEXT,     -- kind = 'text': what was said
    call      TEXT,     -- kinds 'use' and 'result': the id that pairs them
    tool      TEXT,     -- kind = 'use': which tool
    arguments TEXT,     -- kind = 'use': the raw JSON, unparsed here as anywhere
    content   TEXT,     -- kind = 'result': what the model is shown
    failed    INTEGER,  -- kind = 'result': whether that is an explanation
    UNIQUE (message, ordinal)
) STRICT;
