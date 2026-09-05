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

	invoke    hold an exchange with a model and print what was said
	help      print this

invoke takes one turn with -message, or reads them from stdin a line at a
time if there is none. What the run is bounded by is the caller's to choose:
-model and -max-tokens for the model to reach and how much of a reply it may
write, -max-retries for how many times a request the API turned away is sent
again, -workspace for the directory the file tools may see, and -max-turns for
how many turns one exchange may take. An absent one of those is the value that
was true before there was a flag to say otherwise.

Run compadre invoke -h for the arguments invoke understands, and the defaults
they stand in for.

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
