// Command compadre is the command line to the agent runtime. It parses the
// arguments into a command and runs it; everything else lives behind the
// core's ports.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ksahli/compadre/commands"
)

// run is main with its edges handed to it: the arguments to parse and the
// writer to complain to, and an exit code rather than a call to [os.Exit].
// Everything the binary decides happens here, where a test can reach it.
func run(arguments []string, stderr io.Writer) int {
	if len(arguments) < 1 {
		fmt.Fprintln(stderr, "missing command, use compadre help for more details")
		return 1
	}

	command, err := commands.New(arguments)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	ctx := context.Background()
	if err := command.Execute(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}
