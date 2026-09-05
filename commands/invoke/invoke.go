// Package invoke takes one turn with a model and prints what was said.
//
// This is where a provider is chosen, the tools it may offer are assembled,
// and the store the record is kept in is opened — which makes it the wiring,
// the other half of the pair that knows an adapter exists. It assembles those,
// opens an exchange, and hands them to an agent; running the exchange is
// [github.com/ksahli/compadre/internal/core/agents]'s work, not a command's.
//
// What is left here is the half that is genuinely a command's: choosing the
// provider and the model it reaches with the ceiling on one reply and the
// retries it is worth, deciding which tools are on offer and what root the file
// ones may see, saying where the record goes, saying how many turns a run may
// take, and printing.
//
// Five of those are flags with a default rather than a fixed answer. -model,
// -max-tokens and -max-retries are the adapter's to hold and are named at
// [anthropic.New]; -max-turns is the agent's and is named at [agents.New];
// -workspace moves the root the file tools are held inside off the directory
// the command was run in. An absent one of any of them is not an absence but
// the value that was true before there was a flag, which is why each default is
// a named constant somewhere rather than a literal here. The agent keeps the
// record but hands the exchange back rather than showing it to anyone, so the
// words reach a reader only because this package puts them there.
//
// The turn is -message and there has to be one. It opens the exchange, the
// loop runs it to its end, and the command is done — one turn is the whole of
// the run, which is what makes everything below simpler than it was when the
// same package also held a conversation. A caller with more than one thing to
// ask wants compadre interact, which reads its turns from stdin and keeps the
// prompt between them.
//
// An interrupt ends the run, since the turn it landed in was the whole of it.
// This package listens for it rather than the process it is a command of, so
// that what the model got as far as saying is still printed and the exchange is
// still reported: the request is abandoned rather than the process killed
// mid-flight. A turn that failed ends the run the same way and for the same
// reason.
//
// Opening the exchange has two shapes. Without -exchange it is a new one,
// opened with the instructions given here. With one it is the exchange filed
// under that id, read back out of the store and carried on — the same run
// either way, since the loop is handed an exchange rather than asked to start
// one.
//
// The two streams say different things. What the model said goes to stdout,
// and only that, so a caller can pipe the answer somewhere without picking it
// out of anything else. Where the exchange was filed goes to stderr, and so
// does what the turn cost when -usage asks for it, because both are this
// program talking about itself rather than the answer to what was asked.
package invoke

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	Agent    = agents.Type
	Exchange = exchanges.Type
	Store    = memory.Store
)

// Command is a parsed invoke: the instructions to stand over the exchange, the
// message to carry it on if there is one, the exchange to continue if there is
// one, where to keep the record of it, what the run is bounded by — the model,
// the ceiling on one reply, the retries of a request the API turned away, the
// directory the file tools may see and the ceiling on the turns — whether what
// each turn cost is to be said, and the three streams it is reached through.
//
// The five bounds are kept as the caller gave them, zero where they were not
// given, and the defaults are reached for at [Command.Execute] where the
// things that own them are built. Filling them in here would put this
// package's name on numbers that are not its to state.
//
// Whether the counts are said sits with the streams rather than with the
// bounds. It is not something the run is held to; it is a question about what
// this program says about itself, which is the same question the streams
// answer.
//
// The streams are fields rather than named where they are used so that a run
// can be driven by a test without a terminal at one end of it or an API key at
// the other.
type Command struct {
	instructions string
	input        string
	exchange     string
	store        string

	model     string
	tokens    int64
	retries   int
	directory string
	turns     int

	usage bool

	out  io.Writer
	errs io.Writer
}

// Execute assembles the exchange and runs it: the tools on offer, the provider
// to reach the model through, and the exchange to carry on — a new one, or the
// one -exchange names. Then the one turn -message asked for.
//
// One thing is settled before any of that. An exchange that already exists
// carries the instructions it was opened with, and they are not this run's to
// replace. Saying so beats taking the flag and ignoring it, which is the only
// other way for a caller who passed both to find out which one won.
func (c *Command) Execute(ctx context.Context) error {
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
	agent := agents.New(anthropic.New(c.model, c.tokens, c.retries), registry, store, c.turns)

	// The exchange comes back whether the run ended well or badly, so what
	// was said is printed either way: a run that failed on its fourth turn
	// still said three turns' worth of things, and losing those would be
	// reporting the failure by throwing away the answer. The same goes for
	// where it was filed — a run that failed still left a record, and the
	// id is how a reader finds it, which is what makes a session that ended
	// on an error something to be picked back up rather than lost.
	//
	// One interrupt is held at a time, which is all the one waiter can act
	// on: the buffer is what keeps a signal that arrives before the turn
	// has begun from being dropped rather than cancelling it.
	//
	// The message is sent with its whitespace on. It was weighed without it
	// at parsing, where a turn of only spaces was refused; what got this far
	// is the caller's own words and is not this command's to tidy.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	turn, stop := interruptible(ctx, interrupts)
	finished, err := c.converse(turn, agent, opening.With(opening.Thread().Append(ask(c.input))))
	stop()

	if id := finished.ID(); id != "" {
		fmt.Fprintf(c.errs, "exchange %s in %s\n", id, path)
	}

	return err
}

// converse runs the turn to its end and prints what the model said in it.
//
// Where the turn began is taken before it starts, because it is the only
// moment anything knows it: once the loop has folded its turns in there is
// nothing in the thread that says which of them are new. A resumed exchange
// arrives with turns already in it, so counting is what keeps them from being
// read back to a caller who has seen them.
func (c *Command) converse(ctx context.Context, agent Agent, exchange Exchange) (Exchange, error) {
	said := len(exchange.Thread().Messages())
	finished, err := agent.Converse(ctx, exchange)
	transcribe(c.out, c.errs, finished.Thread(), said, c.usage)
	return finished, err
}

// open is the exchange this run is to carry on: the one -exchange names, or a
// new one opened with the instructions given here. Either way what comes back
// is an exchange a turn can be folded onto, which is why the difference
// between the two ends here rather than being carried any further in.
//
// An id nothing was filed under comes back as the store's error rather than a
// fresh exchange. A caller who named one meant that one, and quietly opening a
// different conversation because the named one could not be found is the
// answer to a question nobody asked.
func (c *Command) open(ctx context.Context, store Store) (Exchange, error) {
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

// interruptible is the context one turn runs under: ctx, cancelled by the next
// interrupt, and the stop that releases the watching goroutine once the turn is
// over. It is what makes an interrupt the end of a turn rather than the end of
// everything — a context is cancelled once and for all, so a session that ran
// its turns under one would have no more turns after the first Ctrl-C.
//
// The stop says whether it was the interrupt that ended the turn, because it is
// the only thing that knows. The context cannot be asked afterwards: stopping
// cancels it too, so by the time the caller is holding an answer every turn
// looks cancelled. Nor can the error, which is whatever the provider made of a
// request that went away.
//
// The stop waits for the watcher to be gone before it returns, which is what
// keeps an interrupt from falling between two waits. A watcher still on its way
// out is still a candidate to receive one, and a signal received by nobody who
// will act on it is a keystroke that did nothing; waiting means that once the
// turn is over the only thing listening is the prompt.
func interruptible(ctx context.Context, interrupts <-chan os.Signal) (context.Context, func() bool) {
	running, cancel := context.WithCancel(ctx)

	aborted, watched := false, make(chan struct{})
	go func() {
		defer close(watched)

		select {
		case <-interrupts:
			aborted = true
			cancel()
		case <-running.Done():
		}
	}()

	return running, func() bool {
		cancel()
		<-watched
		return aborted
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
// in it before this turn — a resumed exchange's earlier runs — and the caller
// asked a question rather than for the
// conversation to be read back to them, so what was already there is skipped.
// A thread only ever grows, so from is never past the end; it is clamped
// anyway, because it arrives from outside rather than being counted here.
//
// What a turn cost is said only when the caller asked for it, and never on the
// stream the answer is on: it is this program accounting for the run rather
// than something the model said, so it goes where the prompt and the exchange
// id go. A turn nobody counted says nothing, which is not the same as a turn
// counted at nothing.
func transcribe(out, errs io.Writer, thread threads.Type, from int, counts bool) {
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

		if count := message.Usage(); counts && count.Counted() {
			fmt.Fprintf(errs, "tokens: %d in, %d out\n", count.Input(), count.Output())
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
// -model for the model to reach, -max-tokens for the ceiling on one reply it
// may write and -max-retries for how many times a request the API turned away
// is sent again, -workspace for the directory the file tools may see, and
// -max-turns for how many turns one exchange may take.
//
// An absent -store is not a run that keeps no record: it is a run that keeps
// one where this program keeps them by default, and an absent -exchange is a
// run that opens one rather than one that keeps nothing. The same holds for
// the five bounds — an absent one of them is the value that was true before
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
// asked. Zero is the absence above and is left alone, -max-retries included:
// asking for no retries at all is a thing a caller might mean, but it is not a
// thing this flag can say, because saying it would cost every bound here the
// one rule they share about what an absent one is.
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
	f.StringVar(&c.input, "message", "", "the turn to send")
	f.StringVar(&c.exchange, "exchange", "", "the exchange to continue, from a previous run")
	f.StringVar(&c.store, "store", "", "where to keep the record of the exchange")
	f.StringVar(&c.model, "model", "", "the model to reach, "+anthropic.Model+" if absent")
	f.Int64Var(&c.tokens, "max-tokens", 0, fmt.Sprintf("ceiling on one reply, %d if absent", anthropic.Tokens))
	f.IntVar(&c.retries, "max-retries", 0, fmt.Sprintf("retries of a request the API turned away, %d if absent", anthropic.Retries))
	f.StringVar(&c.directory, "workspace", "", "the directory the file tools may see, the current one if absent")
	f.IntVar(&c.turns, "max-turns", 0, fmt.Sprintf("ceiling on the turns in one exchange, %d if absent", agents.Turns))
	f.BoolVar(&c.usage, "usage", false, "say what each turn of the exchange cost, in tokens")

	if err := f.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errors.New(strings.TrimRight(usage.String(), "\n"))
		}
		return nil, err
	}

	if strings.TrimSpace(c.input) == "" {
		return nil, errors.New("invoke has nothing to say, pass a turn with -message, or hold a conversation with compadre interact")
	}
	if c.tokens < 0 {
		return nil, fmt.Errorf("-max-tokens must be a positive number of tokens, got %d", c.tokens)
	}
	if c.turns < 0 {
		return nil, fmt.Errorf("-max-turns must be a positive number of turns, got %d", c.turns)
	}
	if c.retries < 0 {
		return nil, fmt.Errorf("-max-retries must be a positive number of retries, got %d", c.retries)
	}

	c.out, c.errs = os.Stdout, os.Stderr

	return c, nil
}
