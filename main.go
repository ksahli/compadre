// Command compadre is the command line to the agent runtime. It parses the
// arguments into a command and runs it; everything else lives behind the
// core's ports.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ksahli/compadre/commands"
)

// run is main with its edges handed to it: the arguments to parse and the
// writer to complain to, and an exit code rather than a call to [os.Exit].
// Everything the binary decides happens here, where a test can reach it.
func run(arguments []string, stderr io.Writer) int {
	command, err := commands.New(arguments)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	// A termination cancels the whole run: the context is threaded through
	// every call the command makes, so there is somewhere for the signal to
	// land rather than the process dying mid-request.
	//
	// An interrupt is deliberately not here. What Ctrl-C means depends on
	// what is driving the turns — the end of the run when there is one
	// message, the end of the turn when there is a session at a prompt —
	// and that is the command's to know. It is also why this context could
	// not answer it if it wanted to: cancelling is once and for all, and a
	// session has to survive being interrupted.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	if err := command.Execute(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}
