package help

import (
	"context"
	"fmt"
	"io"
	"os"
)

type Context = context.Context

// usage is what the caller reads. One line per command, in the order someone
// meeting the binary would want them: what to run, then how to ask again.
const usage = `compadre is an agent runtime.

Usage:

	compadre <command> [arguments]

Commands:

	invoke      take one turn with a model and print what was said
	interact    hold an exchange with a model at a prompt, a turn at a time
	help        print this

The two say the same things to the same model and differ in where the turns
come from. invoke takes one with -message and is done. interact reads them
from stdin a line at a time and answers each in the exchange the last one
grew, so a conversation is one process under one id; the way out is the end of
the input, or two Ctrl-Cs in a row at the prompt.

What the run is bounded by is the caller's to choose, and both take the same
flags for it: -model and -max-tokens for the model to reach and how much of a
reply it may write, -max-retries for how many times a request the API turned
away is sent again, -workspace for the directory the file tools may see, and
-max-turns for how many turns one exchange may take. An absent one of those is
the value that was true before there was a flag to say otherwise.

-exchange picks up an exchange a previous run filed, in either command, and
-store says where the record is kept.

-usage says what each turn cost, in tokens read and tokens written. It goes to
stderr with everything else the program says about itself, so the answer can
still be piped somewhere on its own.

Run compadre invoke -h or compadre interact -h for the arguments each
understands, and the defaults they stand in for.

The Anthropic API key is read from ANTHROPIC_API_KEY.`

// Command is a parsed help: the writer to print to, and nothing else.
type Command struct {
	out io.Writer
}

// Execute prints the usage. The context is unused: nothing here can be waited
// on, and nothing here can fail in a way the caller could act on.
func (c *Command) Execute(_ Context) error {
	_, err := fmt.Fprintln(c.out, usage)
	return err
}

// New parses the arguments help understands, which is none of them: anything
// after the name is ignored rather than refused, because a caller reaching for
// help is already unsure what to type and turning that into an error would be
// unkind.
func New(_ []string) (*Command, error) {
	return &Command{out: os.Stdout}, nil
}
