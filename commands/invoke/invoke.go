// Package invoke runs one exchange with a model to its end and prints what
// was said.
//
// This is where a provider is chosen and the tools it may offer are
// assembled, which makes it the wiring — the other half of the pair that
// knows an adapter exists. It assembles those, opens a thread with the message
// it was given, and hands all three to an agent; running the exchange is
// [github.com/ksahli/compadre/internal/core/agents]'s work, not a command's.
//
// What is left here is the half that is genuinely a command's: choosing the
// provider, deciding which tools are on offer and what root the file ones may
// see, and printing. The agent hands back the exchange rather than writing it
// anywhere, so the words reach a reader only because this package puts them
// there.
package invoke

import (
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
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/providers/anthropic"
	"github.com/ksahli/compadre/internal/tools/files"
	"github.com/ksahli/compadre/internal/tools/weather"
	"github.com/ksahli/compadre/internal/tools/web"
)

type (
	Context = context.Context
)

// Command is a parsed invoke: the instructions to stand over the exchange,
// and the message to open it with.
type Command struct {
	instructions string
	input        string
}

// Execute assembles the exchange and runs it: the tools on offer, the
// provider to reach the model through, and the thread to open with. An empty
// message is refused here rather than at parsing, where an absent flag and an
// empty one are the same thing: it is only an exchange with nothing to open it
// that is the mistake, and it is worth saying so before a request goes out to
// be refused by the API in words about content blocks.
func (c *Command) Execute(ctx Context) error {
	if strings.TrimSpace(c.input) == "" {
		return fmt.Errorf("a message is required, pass one with -message")
	}

	root, err := workspace()
	if err != nil {
		return err
	}

	thread := threads.New(c.instructions, messages.New(roles.User, messages.Text(c.input)))
	registry := definitions.New(
		weather.New(),
		web.New(),
		files.NewList(root), files.NewRead(root), files.NewWrite(root),
	)
	provider := anthropic.New()

	// The exchange comes back whether it ended well or badly, so what was
	// said is printed either way: a run that failed on its fourth turn
	// still said three turns' worth of things, and losing those would be
	// reporting the failure by throwing away the answer.
	finished, err := agents.New(provider, registry).Converse(ctx, thread)
	transcribe(os.Stdout, finished)

	return err
}

// transcribe prints what the model said, and only that.
//
// Two things are left out, and for different reasons. A tool call and its
// answer are the exchange's business rather than the reader's — they are how
// the answer was arrived at, not the answer. And the turn that opened the
// exchange is the caller's own message, which they do not need read back to
// them; skipping every role but the assistant's is what settles both the first
// case and any later user turn the loop folded in.
func transcribe(out io.Writer, thread threads.Type) {
	for _, message := range thread.Messages() {
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

// workspace is the directory the file tools may see: where the command was
// run, resolved through symlinks once, here. Once rather than per call so that
// every later question of whether a path is inside it compares two paths
// nothing can still redirect. A failure here ends the command rather than
// being handed to the model — there is no exchange yet to report it into, and
// a process that cannot say where it is has no business offering to list it.
func workspace() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not determine the workspace: %w", err)
	}

	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("could not resolve the workspace: %w", err)
	}

	return root, nil
}

// New parses the arguments invoke understands: -instructions for the system
// instructions, and -message for the turn to send. An argument the flag set
// cannot make sense of comes back as an error: parsing is not the place to
// end the process, and the caller is already reporting.
//
// The flag set writes into a buffer rather than to a stream of its own, for
// the same reason. A request for help is one of the errors Parse can return,
// and the buffer is what makes the usage it wrote available to hand back as
// that error rather than printed from in here.
func New(arguments []string) (*Command, error) {
	c, f := new(Command), flag.NewFlagSet("invoke", flag.ContinueOnError)
	usage := &bytes.Buffer{}
	f.SetOutput(usage)

	f.StringVar(&c.instructions, "instructions", "", "system instructions")
	f.StringVar(&c.input, "message", "", "user message")

	if err := f.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errors.New(strings.TrimRight(usage.String(), "\n"))
		}
		return nil, err
	}

	return c, nil
}
