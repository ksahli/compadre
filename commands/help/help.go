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

	invoke    run one exchange with a model and print what was said
	help      print this

Run compadre invoke -h for the arguments invoke understands.

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
