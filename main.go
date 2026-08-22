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

	// An interrupt cancels the exchange rather than killing the process
	// mid-request: the context is already threaded through every call the
	// command makes, so there is somewhere for the signal to land.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
