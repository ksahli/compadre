package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/thoughts"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools/results"
	"github.com/ksahli/compadre/internal/core/tools/use"

	_ "modernc.org/sqlite"
)

type (
	Context  = context.Context
	Exchange = exchanges.Type
	Message  = messages.Type
	Content  = messages.Content
)

//go:embed schema.sql
var schema string

//go:embed rebuild.sql
var rebuild string

// The shapes a content block can be, spelled the way the CHECK in schema.sql
// spells them. They are the schema's own words rather than the core's, which
// is why they live here and not in messages.
const (
	kindText     = "text"
	kindThinking = "thinking"
	kindRedacted = "redacted"
	kindUse      = "use"
	kindResult   = "result"
)

// Store keeps exchanges in a SQLite database.
type Store struct {
	db *sql.DB
}

// New opens the database at path, creating it if it is not there, and applies
// the schema. Applying it to a database that already has it is not an error,
// so opening an existing store and opening a fresh one are the same call.
//
// The pragmas are set on the connection string rather than run afterwards,
// because a pool opens connections when it feels like it and one that had not
// been told about foreign keys would enforce none.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("could not open the store at '%s': %w", path, err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not prepare the store at '%s': %w", path, err)
	}

	if err := widen(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not prepare the store at '%s': %w", path, err)
	}

	return &Store{db: db}, nil
}

// widen brings a database written before thinking was a content shape up to
// the schema this package now applies, and leaves one that is already there
// alone.
//
// It exists because the schema is applied with CREATE TABLE IF NOT EXISTS,
// which leaves an existing table exactly as it was, CHECK and all. A store
// filed under the old definition would keep taking every turn it always took
// and refuse the one new one, which is the worst of the outcomes on offer: not
// a failed open, not a working store, but an exchange that stops being
// writable the moment the model reasons.
//
// What it reads is the table's own definition, because that is the thing that
// has to change and the only thing that says whether it already has. See
// rebuild.sql for what the rebuild does and why it is a rebuild.
func widen(db *sql.DB) error {
	var definition string
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'contents'`).Scan(&definition)
	if err != nil {
		return fmt.Errorf("could not read the shape of the record: %w", err)
	}

	if strings.Contains(definition, "'"+kindThinking+"'") {
		return nil
	}

	if _, err := db.Exec(rebuild); err != nil {
		return fmt.Errorf("could not bring the record up to date: %w", err)
	}

	return nil
}

// dsn is the path with the settings the store needs to work: foreign keys
// enforced, so a message can never outlive the thread it belongs to; the
// write-ahead log, so a read never waits on the turn being written; and a busy
// timeout, so two processes writing at once wait for each other rather than
// one of them failing outright.
func dsn(path string) string {
	pragmas := url.Values{"_pragma": {
		"foreign_keys(1)",
		"journal_mode(WAL)",
		"busy_timeout(5000)",
	}}
	return "file:" + path + "?" + pragmas.Encode()
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Save writes the exchange as it now stands and returns it filed under its id.
// An exchange arriving without one is opened here and given the row's, which
// is why an exchange comes back rather than nothing.
//
// Only the turns the store has not seen are written. An exchange grows by
// appending and never any other way, so what is already on disk is already
// right, and counting the messages filed under the thread is enough to know
// where the new ones start. It is all one transaction: a turn that is half
// written down is a record of something that did not happen.
func (s *Store) Save(ctx Context, exchange Exchange) (Exchange, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return exchange, fmt.Errorf("could not begin writing the exchange: %w", err)
	}
	defer tx.Rollback()

	thread := exchange.Thread()

	id, written, err := open(ctx, tx, exchange)
	if err != nil {
		return exchange, err
	}

	conversation := thread.Messages()
	if written > len(conversation) {
		return exchange, fmt.Errorf(
			"the exchange filed under '%s' holds %d turns, more than the %d being saved: an exchange only ever grows",
			exchange.ID(), written, len(conversation))
	}

	for ordinal, message := range conversation[written:] {
		if err := write(ctx, tx, id, written+ordinal, message); err != nil {
			return exchange, err
		}
	}

	if err := tx.Commit(); err != nil {
		return exchange, fmt.Errorf("could not finish writing the exchange: %w", err)
	}

	return exchanges.New(strconv.FormatInt(id, 10), thread), nil
}

// open finds the row the exchange is filed under, or makes one, and says how
// many of its turns are already written down.
func open(ctx Context, tx *sql.Tx, exchange Exchange) (int64, int, error) {
	if exchange.ID() == "" {
		row, err := tx.ExecContext(ctx,
			`INSERT INTO threads (instructions, opened_at) VALUES (?, ?)`,
			exchange.Thread().Instructions(), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return 0, 0, fmt.Errorf("could not open the exchange: %w", err)
		}
		id, err := row.LastInsertId()
		if err != nil {
			return 0, 0, fmt.Errorf("could not open the exchange: %w", err)
		}
		return id, 0, nil
	}

	id, err := identify(exchange.ID())
	if err != nil {
		return 0, 0, err
	}

	var written int
	err = tx.QueryRowContext(ctx,
		`SELECT count(*) FROM messages WHERE thread = ?`, id).Scan(&written)
	if err != nil {
		return 0, 0, fmt.Errorf("could not read the exchange filed under '%s': %w", exchange.ID(), err)
	}

	return id, written, nil
}

// write puts one turn and everything it says into the record.
func write(ctx Context, tx *sql.Tx, thread int64, ordinal int, message Message) error {
	row, err := tx.ExecContext(ctx,
		`INSERT INTO messages (thread, ordinal, role) VALUES (?, ?, ?)`,
		thread, ordinal, message.Role())
	if err != nil {
		return fmt.Errorf("could not write turn %d of the exchange: %w", ordinal, err)
	}

	id, err := row.LastInsertId()
	if err != nil {
		return fmt.Errorf("could not write turn %d of the exchange: %w", ordinal, err)
	}

	for at, content := range message.Content() {
		if err := say(ctx, tx, id, at, content); err != nil {
			return fmt.Errorf("could not write turn %d of the exchange: %w", ordinal, err)
		}
	}

	return nil
}

// say puts one block of a turn into the record, in the columns its shape uses
// and leaving the rest null.
func say(ctx Context, tx *sql.Tx, message int64, ordinal int, content Content) error {
	const statement = `INSERT INTO contents
		(message, ordinal, kind, text, signature, call, tool, arguments, content, failed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	if text, ok := content.Text(); ok {
		_, err := tx.ExecContext(ctx, statement,
			message, ordinal, kindText, text, nil, nil, nil, nil, nil, nil)
		return err
	}
	if thought, ok := content.Thought(); ok {
		// Neither half is read on the way in, as neither is on the way
		// out. What is written down is what arrived, down to a
		// reasoning that is the empty string: that is a whole thought
		// and not half of one.
		if data, redacted := thought.Data(); redacted {
			_, err := tx.ExecContext(ctx, statement,
				message, ordinal, kindRedacted, data, nil, nil, nil, nil, nil, nil)
			return err
		}
		_, err := tx.ExecContext(ctx, statement,
			message, ordinal, kindThinking, thought.Text(), thought.Signature(),
			nil, nil, nil, nil, nil)
		return err
	}
	if request, ok := content.Use(); ok {
		// The arguments are raw JSON and stay that way: the core does
		// not parse them and neither does the record of it.
		_, err := tx.ExecContext(ctx, statement,
			message, ordinal, kindUse, nil, nil,
			request.ID(), request.Name(), string(request.Arguments()), nil, nil)
		return err
	}
	if result, ok := content.Result(); ok {
		_, err := tx.ExecContext(ctx, statement,
			message, ordinal, kindResult, nil, nil,
			result.ID(), nil, nil, result.Content(), result.Failed())
		return err
	}

	// The set is closed by an unexported method, so reaching here means a
	// shape was added to it and this was not taught to write it down.
	return fmt.Errorf("a content block of an unknown shape cannot be written down")
}

// Load returns the exchange filed under an id. An id nothing was filed under
// is an error rather than an empty exchange: a conversation that never
// happened is not one to continue.
func (s *Store) Load(ctx Context, id string) (Exchange, error) {
	thread, err := identify(id)
	if err != nil {
		return Exchange{}, err
	}

	var instructions string
	err = s.db.QueryRowContext(ctx,
		`SELECT instructions FROM threads WHERE id = ?`, thread).Scan(&instructions)
	if errors.Is(err, sql.ErrNoRows) {
		return Exchange{}, fmt.Errorf("no exchange is filed under '%s'", id)
	}
	if err != nil {
		return Exchange{}, fmt.Errorf("could not read the exchange filed under '%s': %w", id, err)
	}

	conversation, err := read(ctx, s.db, thread)
	if err != nil {
		return Exchange{}, fmt.Errorf("could not read the exchange filed under '%s': %w", id, err)
	}

	return exchanges.New(id, threads.New(instructions, conversation...)), nil
}

// read rebuilds the turns of a thread, in the order they were said.
//
// It is one query rather than one per turn: the join gives every block of
// every turn ordered by turn and then by place within it, which is the order
// they have to come back in anyway, so the rows arrive already grouped and the
// walk below only has to notice when the turn changes.
func read(ctx Context, db *sql.DB, thread int64) ([]Message, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.role, c.kind, c.text, c.signature, c.call, c.tool, c.arguments, c.content, c.failed
		  FROM messages m
		  LEFT JOIN contents c ON c.message = m.id
		 WHERE m.thread = ?
		 ORDER BY m.ordinal, c.ordinal`, thread)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversation, content := []Message{}, []Content{}
	var current int64
	var role string

	for rows.Next() {
		var (
			message   int64
			said      string
			kind      sql.NullString
			text      sql.NullString
			signature sql.NullString
			call      sql.NullString
			tool      sql.NullString
			arguments sql.NullString
			answer    sql.NullString
			failed    sql.NullBool
		)
		if err := rows.Scan(&message, &said, &kind, &text, &signature, &call, &tool, &arguments, &answer, &failed); err != nil {
			return nil, err
		}

		if current != 0 && message != current {
			conversation = append(conversation, messages.New(role, content...))
			content = []Content{}
		}
		current, role = message, said

		// A turn that says nothing has one row with no block on it,
		// which the left join is there to keep rather than drop.
		if !kind.Valid {
			continue
		}

		piece, err := block(kind.String, text, signature, call, tool, arguments, answer, failed)
		if err != nil {
			return nil, err
		}
		content = append(content, piece)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if current != 0 {
		conversation = append(conversation, messages.New(role, content...))
	}

	return conversation, nil
}

// block turns one row back into the content it was written from.
func block(kind string, text, signature, call, tool, arguments, answer sql.NullString, failed sql.NullBool) (Content, error) {
	switch kind {
	case kindText:
		return messages.Text(text.String), nil
	case kindThinking:
		// A thought with nothing in either column is a whole thought
		// rather than a broken one: an API asked to keep its reasoning
		// to itself returns exactly that.
		return messages.Thinking(thoughts.New(text.String, signature.String)), nil
	case kindRedacted:
		return messages.Thinking(thoughts.Redacted(text.String)), nil
	case kindUse:
		// A call with no arguments was written as an empty string and
		// comes back as one: nil and empty are the same absence here,
		// and the tool reads neither.
		var raw use.Arguments
		if arguments.String != "" {
			raw = use.Arguments(arguments.String)
		}
		return messages.Use(use.New(call.String, tool.String, raw)), nil
	case kindResult:
		return messages.Result(results.New(call.String, answer.String, failed.Bool)), nil
	}

	// The CHECK on the column says this cannot happen, which is exactly
	// why it is worth saying so rather than returning something plausible.
	return nil, fmt.Errorf("a content block of an unknown kind: '%s'", kind)
}

// identify reads an id back into the row it names. The port speaks in strings
// because what an id is depends on the store; here it is a rowid, and one that
// is not a number was never handed out by this one.
func identify(id string) (int64, error) {
	thread, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("no exchange is filed under '%s'", id)
	}
	return thread, nil
}
