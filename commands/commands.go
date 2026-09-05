// Package commands maps a command line to the command that runs it.
//
// One package per command, each exposing a New that parses its own
// arguments and a value satisfying [Command]. This package knows only the
// names; it does not know what any of them do.
package commands

import (
	"context"
	"fmt"

	"github.com/ksahli/compadre/commands/help"
	"github.com/ksahli/compadre/commands/interact"
	"github.com/ksahli/compadre/commands/invoke"
)

// Command is one thing the binary can be asked to do, already parsed and
// ready to run.
type Command interface {
	Execute(ctx context.Context) error
}

// New reads the command name off the front of the arguments and hands the
// rest to whichever package owns it. An unknown name is an error, not a
// fallback, and so is no name at all: this package holds its own precondition
// rather than trusting the caller to have checked the length first.
func New(arguments []string) (Command, error) {
	if len(arguments) == 0 {
		return nil, fmt.Errorf("missing command, use compadre help for more details")
	}

	name := arguments[0]
	switch name {
	case "invoke":
		return invoke.New(arguments[1:])
	case "interact":
		return interact.New(arguments[1:])
	case "help":
		return help.New(arguments[1:])
	default:
		err := fmt.Errorf("unknown command: '%s'", name)
		return nil, err
	}
}
