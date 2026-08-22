// Package invoke runs one exchange with a model to its end and prints what
// was said.
//
// This is where a provider is chosen and the tools it may offer are
// assembled, which makes it the wiring — the other half of the pair that
// knows an adapter exists. It is also where the loop lives: a reply that asks
// for a tool is run, answered, and sent back, until the model has nothing
// left to ask for.
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

	"github.com/ksahli/compadre/internal/core/inference"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/providers/anthropic"
	"github.com/ksahli/compadre/internal/tools/files"
	"github.com/ksahli/compadre/internal/tools/weather"
)

type (
	Context = context.Context
)

// turns is the ceiling on one exchange. A model that keeps asking for tools
// would otherwise spend without end, and stopping loudly is better than that.
const turns = 10

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
	registry := definitions.New(weather.New(), files.New(root))
	provider := anthropic.New()

	return converse(ctx, provider, registry, thread, os.Stdout)
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

// converse runs the exchange to its end. Each turn the thread goes out, what
// the model said is printed, and its replies are folded back in; anything it
// asked for is run and answered in one message, and round again. The model
// asking for nothing is the end of it. The first error ends the run, and
// nothing of that turn is printed ahead of it: a reply that failed is not a
// reply. Only text is printed — a tool call and its answer are the exchange's
// business, not the reader's.
//
// It takes its provider, its tools and its writer rather than reaching for
// them, so that the loop can be run without a model at the other end.
func converse(ctx Context, provider inference.Provider, registry tools.Registry, thread threads.Type, out io.Writer) error {
	for range turns {
		replies, err := provider.Invoke(ctx, thread, registry)
		if err != nil {
			return err
		}

		requests := []tools.Use{}
		for _, reply := range replies {
			for _, content := range reply.Content() {
				if text, ok := content.Text(); ok {
					fmt.Fprintln(out, text)
				}
				if use, ok := content.Use(); ok {
					requests = append(requests, use)
				}
			}
		}

		// The model's turn has to be in the record before the
		// answers to it are: a result with no call ahead of it is
		// not something the model can read.
		thread = thread.Append(replies...)

		if len(requests) == 0 {
			return nil
		}

		answers := []messages.Content{}
		for _, request := range requests {
			// Invoke does not fail: a tool that did is a result
			// the model reads and recovers from.
			result := tools.Invoke(ctx, request, registry)
			answers = append(answers, messages.Result(result))
		}
		thread = thread.Append(messages.New(roles.User, answers...))
	}

	return fmt.Errorf("gave up after %d turns: the model kept asking for tools", turns)
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
