// Package invoke runs an exchange with a model and prints what was said.
//
// This is where a provider is chosen, the tools it may offer are assembled,
// and the store the record is kept in is opened — which makes it the wiring,
// the other half of the pair that knows an adapter exists. It assembles those,
// opens an exchange, and hands them to an agent; running the exchange is
// [github.com/ksahli/compadre/internal/core/agents]'s work, not a command's.
//
// What is left here is the half that is genuinely a command's: choosing the
// provider and the model it reaches with the ceiling on one reply, deciding
// which tools are on offer and what root the file ones may see, saying where
// the record goes, saying how many turns a run may take, deciding what drives
// the turns, and printing.
//
// Four of those are flags with a default rather than a fixed answer. -model
// and -max-tokens are the adapter's to hold and are named at [anthropic.New];
// -max-turns is the agent's and is named at [agents.New]; -workspace moves the
// root the file tools are held inside off the directory the command was run
// in. An absent one of any of them is not an absence but the value that was
// true before there was a flag, which is why each default is a named constant
// somewhere rather than a literal here. The agent keeps the record but hands the exchange back rather than
// showing it to anyone, so the words reach a reader only because this package
// puts them there.
//
// There are two ways to drive the turns and the difference is one flag. With
// -message it is one turn: the message opens the exchange, the loop runs it to
// its end, and the command is done. Without one it is a session — turns are
// read from stdin a line at a time and each is answered in the exchange the
// last one grew, in one process and under one id. An empty -message and an
// absent one are the same thing to the flag set, so both mean a session; a run
// with nothing to say is a run that will be asked, not an error.
//
// Opening the exchange has two shapes of its own, and they cross with the
// first two rather than doubling them. Without -exchange it is a new one,
// opened with the instructions given here. With one it is the exchange filed
// under that id, read back out of the store and continued — the same run
// either way, since the loop is handed an exchange rather than asked to start
// one, and a session picks one up as readily as a single message does.
//
// The two streams say different things. What the model said goes to stdout,
// and only that, so a caller can pipe the answer somewhere without picking it
// out of anything else. Where the exchange was filed goes to stderr, and so
// does the prompt a session writes, because both are this program talking
// about itself rather than the answer to what was asked. That is what lets a
// session be driven by a pipe as well as by a person, with no test for which
// it is: the prompts go where the answer is not.
package invoke

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ksahli/compadre/internal/core/agents"
	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/memory"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/providers/anthropic"
	"github.com/ksahli/compadre/internal/stores/sqlite"
	"github.com/ksahli/compadre/internal/tools/files"
	"github.com/ksahli/compadre/internal/tools/weather"
	"github.com/ksahli/compadre/internal/tools/web"
)

type (
	Context  = context.Context
	Agent    = agents.Type
	Exchange = exchanges.Type
	Store    = memory.Store
)

const (
	// prompt is what a session writes before waiting for a turn. It goes
	// to stderr, like everything else this program says about itself.
	prompt = "> "

	// maxTurn is the ceiling on one line read from stdin, and scanning the
	// buffer that reading starts with. bufio's own default ceiling is
	// 64KiB, which is a plausible size for a turn somebody pasted rather
	// than typed; a line over this one is still the end of the session,
	// but it is the end that says so.
	maxTurn  = 1 << 20
	scanning = 64 << 10
)

// Command is a parsed invoke: the instructions to stand over the exchange, the
// message to carry it on if there is one, the exchange to continue if there is
// one, where to keep the record of it, what the run is bounded by — the model,
// the ceiling on one reply, the directory the file tools may see and the
// ceiling on the turns — and the three streams it is reached through.
//
// The four bounds are kept as the caller gave them, zero where they were not
// given, and the defaults are reached for at [Command.Execute] where the
// things that own them are built. Filling them in here would put this
// package's name on numbers that are not its to state.
//
// The streams are fields rather than named where they are used so that a
// session can be driven by a test without a terminal at one end of it or an
// API key at the other.
type Command struct {
	instructions string
	input        string
	exchange     string
	store        string

	model     string
	tokens    int64
	directory string
	turns     int

	in   io.Reader
	out  io.Writer
	errs io.Writer
}

// Execute assembles the exchange and runs it: the tools on offer, the provider
// to reach the model through, and the exchange to carry on — a new one, or the
// one -exchange names. Then either the one turn -message asked for or the
// session that reads them from stdin.
//
// One thing is settled before any of that. An exchange that already exists
// carries the instructions it was opened with, and they are not this run's to
// replace. Saying so beats taking the flag and ignoring it, which is the only
// other way for a caller who passed both to find out which one won.
func (c *Command) Execute(ctx Context) error {
	if c.exchange != "" && c.instructions != "" {
		return fmt.Errorf("the exchange filed under '%s' has its own instructions, -instructions cannot be given with -exchange", c.exchange)
	}

	root, err := workspace(c.directory)
	if err != nil {
		return err
	}

	path, err := c.record()
	if err != nil {
		return err
	}

	store, err := sqlite.New(path)
	if err != nil {
		return err
	}
	defer store.Close()

	opening, err := c.open(ctx, store)
	if err != nil {
		return err
	}

	registry := definitions.New(
		weather.New(),
		web.New(),
		files.NewList(root), files.NewRead(root), files.NewWrite(root),
	)
	agent := agents.New(anthropic.New(c.model, c.tokens), registry, store, c.turns)

	// The exchange comes back whether the run ended well or badly, so what
	// was said is printed either way: a run that failed on its fourth turn
	// still said three turns' worth of things, and losing those would be
	// reporting the failure by throwing away the answer. The same goes for
	// where it was filed — a run that failed still left a record, and the
	// id is how a reader finds it, which is what makes a session that ended
	// on an error something to be picked back up rather than lost.
	//
	// The message is weighed with its whitespace off and sent with it on. A
	// turn that is only spaces is no turn at all and means a session, the
	// same as an absent flag; one that has something in it is the caller's
	// own words and is not this command's to tidy.
	finished := opening
	if strings.TrimSpace(c.input) != "" {
		finished, err = c.converse(ctx, agent, opening.With(opening.Thread().Append(ask(c.input))))
	} else {
		finished, err = c.session(ctx, agent, opening)
	}

	if id := finished.ID(); id != "" {
		fmt.Fprintf(c.errs, "exchange %s in %s\n", id, path)
	}

	return err
}

// session reads turns from stdin and answers each in the exchange the last one
// grew. It is the same loop the command line already ran across processes with
// -exchange, with the store standing in for memory; here the exchange never
// leaves, so nothing is read back and the id is minted once.
//
// A blank line is not a turn. An exchange with nothing to carry it on is the
// mistake -message was refused for, and at a prompt the kindest answer is to
// ask again rather than to spend a request finding out.
//
// The end comes four ways and they are not the same. Stdin running out is the
// end of a session that said everything it had to say, and so is a cancelled
// context — a caller who interrupts at the prompt wants out, and there is
// nothing in flight for the signal to spoil. A turn that failed is the third,
// and it comes back as an error: the record has everything up to it and the id
// is about to be printed, so the session is picked up with -exchange rather
// than carried on over a provider or a store that has stopped working. Stdin
// itself failing is the fourth and comes back as an error too, for the reason
// the two must not be confused: a reader that broke halfway through a line has
// swallowed a turn somebody typed, and reporting that as a session that ended
// would be reporting a lost turn as a job well done.
func (c *Command) session(ctx Context, agent Agent, exchange Exchange) (Exchange, error) {
	lines := listen(c.in)

	for {
		fmt.Fprint(c.errs, prompt)

		next, ok := read(ctx, lines)
		if !ok {
			return exchange, nil
		}
		if next.err != nil {
			return exchange, next.err
		}

		said := strings.TrimSpace(next.line)
		if said == "" {
			continue
		}

		finished, err := c.converse(ctx, agent, exchange.With(exchange.Thread().Append(ask(said))))
		if err != nil {
			return finished, err
		}
		exchange = finished
	}
}

// converse runs one turn to its end and prints what the model said in it.
//
// Where the turn began is taken before it starts, because it is the only
// moment anything knows it: once the loop has folded its turns in there is
// nothing in the thread that says which of them are new. That holds for the
// second turn of a session for the same reason it holds for a resumed
// exchange, which is why one function serves both.
func (c *Command) converse(ctx Context, agent Agent, exchange Exchange) (Exchange, error) {
	said := len(exchange.Thread().Messages())
	finished, err := agent.Converse(ctx, exchange)
	transcribe(c.out, finished.Thread(), said)
	return finished, err
}

// open is the exchange this run is to carry on: the one -exchange names, or a
// new one opened with the instructions given here. Either way what comes back
// is an exchange a turn can be folded onto, which is why the difference
// between the two ends here rather than being carried any further in.
//
// The message is not folded in here, and that is what lets a session use the
// same function: a session has no message yet, and an exchange with no turns
// in it is a perfectly good thing to hand the first one to.
//
// An id nothing was filed under comes back as the store's error rather than a
// fresh exchange. A caller who named one meant that one, and quietly opening a
// different conversation because the named one could not be found is the
// answer to a question nobody asked.
func (c *Command) open(ctx Context, store Store) (Exchange, error) {
	if c.exchange == "" {
		return exchanges.Open(threads.New(c.instructions)), nil
	}

	return store.Load(ctx, c.exchange)
}

// record is the file the exchange is written to: what -store said, or a
// database of this program's own under the user's config directory.
//
// The default is not in the workspace, and that is deliberate. The workspace
// is the boundary the file tools are held inside — it is about what the model
// may touch — and the record of an exchange is the program's, not the
// project's. A run in a directory the caller happens to be passing through
// should not leave a database behind in it.
func (c *Command) record() (string, error) {
	if c.store != "" {
		return c.store, nil
	}

	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not find where to keep the record, pass a path with -store: %w", err)
	}

	directory := filepath.Join(config, "compadre")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("could not make somewhere to keep the record: %w", err)
	}

	return filepath.Join(directory, "exchanges.db"), nil
}

// ask is one turn from the caller, in the core's own terms.
func ask(turn string) messages.Type {
	return messages.New(roles.User, messages.Text(turn))
}

// turn is one line off the reader, or the reason there will not be another.
// The two travel together because the session has to tell them apart, and a
// closed channel says only that nothing more is coming rather than why.
type turn struct {
	line string
	err  error
}

// listen reads lines off a reader into a channel, closed when there are no
// more. It exists so that waiting for a turn is something a select can be
// written over: a read from stdin cannot be cancelled, and a session that
// could only notice an interrupt after the next line was typed would be a
// session with no way out of it.
//
// A reader that failed sends that before it closes. Scanning is the one thing
// here that can end a session without the session having ended — a line past
// the ceiling stops the scan with an error, and a channel that only ever
// closes would hand that to the caller as stdin having run out.
//
// The goroutine is left behind when a session ends on a cancelled context,
// still blocked on a read or a send that will never be looked at. That is
// deliberate and it is bounded: there is one per command, the command is the
// process, and a process on its way out has nothing left for that read to
// interfere with.
func listen(in io.Reader) <-chan turn {
	turns := make(chan turn)

	go func() {
		defer close(turns)

		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 0, scanning), maxTurn)

		for scanner.Scan() {
			turns <- turn{line: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			turns <- turn{err: fmt.Errorf("could not read the turn: %w", err)}
		}
	}()

	return turns
}

// read is the next turn, or the end of the session. The second return says
// which: false is stdin running out or the context being cancelled, and the
// caller treats those alike because they are alike — both are a session that
// is over, neither is a session that went wrong. A session that did go wrong
// arrives as a turn carrying an error rather than as a false, which is the
// distinction the channel is a struct for.
func read(ctx Context, turns <-chan turn) (turn, bool) {
	select {
	case next, ok := <-turns:
		return next, ok
	case <-ctx.Done():
		return turn{}, false
	}
}

// transcribe prints what the model said in this turn, and only that.
//
// Two things are left out, and for different reasons. A tool call and its
// answer are the exchange's business rather than the reader's — they are how
// the answer was arrived at, not the answer. And the turn that opened the
// exchange is the caller's own message, which they do not need read back to
// them; skipping every role but the assistant's is what settles both the first
// case and any later user turn the loop folded in.
//
// The third is what from is for. An exchange arrives carrying everything said
// in it before this turn — a session's earlier rounds, a resumed exchange's
// earlier runs — and the caller asked a question rather than for the
// conversation to be read back to them, so what was already there is skipped.
// A thread only ever grows, so from is never past the end; it is clamped
// anyway, because it arrives from outside rather than being counted here.
func transcribe(out io.Writer, thread threads.Type, from int) {
	conversation := thread.Messages()
	if from > len(conversation) {
		from = len(conversation)
	}

	for _, message := range conversation[from:] {
		if message.Role() != roles.Assistant {
			continue
		}
		for _, content := range message.Content() {
			if text, ok := content.Text(); ok {
				fmt.Fprintln(out, text)
			}
		}
	}
}

// workspace is the directory the file tools may see: what -workspace named, or
// where the command was run if it named nothing. Either way it is resolved
// through symlinks once, here. Once rather than per call so that every later
// question of whether a path is inside it compares two paths nothing can still
// redirect. A failure here ends the command rather than being handed to the
// model — there is no exchange yet to report it into, and a process that
// cannot say where it is has no business offering to list it.
//
// A path that is not a directory is refused by name. The tools would hold the
// model inside it perfectly well — a file contains nothing, so every request
// would be told no — and a run in which every listing is empty for a reason
// nobody is told is a worse answer than saying what was wrong with the
// argument.
func workspace(directory string) (string, error) {
	root := directory
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not determine the workspace: %w", err)
		}
		root = current
	}

	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("could not resolve the workspace: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("could not resolve the workspace: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("the workspace must be a directory, got '%s'", directory)
	}

	return root, nil
}

// New parses the arguments invoke understands: -instructions for the system
// instructions, -message for a single turn to send, -exchange for an exchange
// to continue rather than open, -store for where to keep the record of it,
// -model for the model to reach and -max-tokens for the ceiling on one reply
// it may write, -workspace for the directory the file tools may see, and
// -max-turns for how many turns one exchange may take.
//
// An absent -store is not a run that keeps no record: it is a run that keeps
// one where this program keeps them by default, and an absent -exchange is a
// run that opens one rather than one that keeps nothing. The same holds for
// the four bounds — an absent one of them is the value that was true before
// there was a flag to say otherwise, which is why the zero value stands for
// absence and the default is reached for at [Command.Execute]. An absent
// -message is neither: it is a run that reads its turns from stdin instead of
// from the command line. An argument the flag set cannot make sense of comes
// back as an error: parsing is not the place to end the process, and the
// caller is already reporting.
//
// A negative ceiling is refused here, and that is this function's own check
// rather than the flag set's: -3 parses perfectly well as a number and means
// nothing as a bound. It is refused rather than clamped for the reason an
// unparseable flag is refused — a caller who typed it meant something, and
// quietly running with a different number is the answer to a question nobody
// asked. Zero is the absence above and is left alone.
//
// The flag set writes into a buffer rather than to a stream of its own, for
// the same reason. A request for help is one of the errors Parse can return,
// and the buffer is what makes the usage it wrote available to hand back as
// that error rather than printed from in here.
//
// The streams are settled here too. They are the real ones, and this is the
// only place that names them, so that everything downstream of parsing can be
// run against something else.
func New(arguments []string) (*Command, error) {
	c, f := new(Command), flag.NewFlagSet("invoke", flag.ContinueOnError)
	usage := &bytes.Buffer{}
	f.SetOutput(usage)

	f.StringVar(&c.instructions, "instructions", "", "system instructions")
	f.StringVar(&c.input, "message", "", "user message, or read them from stdin if absent")
	f.StringVar(&c.exchange, "exchange", "", "the exchange to continue, from a previous run")
	f.StringVar(&c.store, "store", "", "where to keep the record of the exchange")
	f.StringVar(&c.model, "model", "", "the model to reach, "+anthropic.Model+" if absent")
	f.Int64Var(&c.tokens, "max-tokens", 0, fmt.Sprintf("ceiling on one reply, %d if absent", anthropic.Tokens))
	f.StringVar(&c.directory, "workspace", "", "the directory the file tools may see, the current one if absent")
	f.IntVar(&c.turns, "max-turns", 0, fmt.Sprintf("ceiling on the turns in one exchange, %d if absent", agents.Turns))

	if err := f.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errors.New(strings.TrimRight(usage.String(), "\n"))
		}
		return nil, err
	}

	if c.tokens < 0 {
		return nil, fmt.Errorf("-max-tokens must be a positive number of tokens, got %d", c.tokens)
	}
	if c.turns < 0 {
		return nil, fmt.Errorf("-max-turns must be a positive number of turns, got %d", c.turns)
	}

	c.in, c.out, c.errs = os.Stdin, os.Stdout, os.Stderr

	return c, nil
}
