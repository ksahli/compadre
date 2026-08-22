package tools

import (
	"context"
	"fmt"

	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/results"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

type (
	Use        = use.Type
	Definition = definitions.Type
	Result     = results.Type
	Registry   = definitions.Map
)

// Success is the answer to a call that ran.
func Success(id, content string) Result {
	return results.New(id, content, false)
}

// Failure is the answer to a call that did not. The error's text is the
// content, because the model is the one that has to read it and try again.
func Failure(id string, err error) Result {
	return results.New(id, err.Error(), true)
}

// Invoke runs the tool the use names and returns the answer to it. There is
// no error return, and that is the point: a name in the set that isn't, and a
// tool that fails, are both things the model must be shown rather than things
// that stop the program.
func Invoke(ctx context.Context, use Use, registry Registry) Result {
	definition, ok := registry[use.Name()]
	if !ok {
		return Failure(use.ID(), fmt.Errorf("unknown tool: '%s'", use.Name()))
	}

	content, err := definition.Execute(ctx, use.Arguments())
	if err != nil {
		return Failure(use.ID(), err)
	}

	return Success(use.ID(), content)
}
