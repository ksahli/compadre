package definitions

import (
	"context"

	"github.com/ksahli/compadre/internal/core/tools/use"
)

// Type represents a tool: what the model is told about it, and what happens
// when the model asks for it. Both halves sit on the one interface, because
// a description without the behaviour behind it is a promise nothing keeps.
type Type interface {
	// Name is what the model calls the tool by. It is the key a [use.Type]
	// is resolved through, so it has to be unique within a [Map].
	Name() string
	// Description is what the model reads to decide whether this is the
	// tool it wants.
	Description() string
	// Schema is the JSON Schema for the arguments, as a plain map. A map
	// rather than a typed value because it is data on its way to a wire
	// format this package is not entitled to know the shape of.
	Schema() map[string]any
	// Execute runs the tool and returns what the model should be shown.
	// The error is not fatal:
	// [github.com/ksahli/compadre/internal/core/tools.Invoke] turns it into
	// a failed result the model can read and recover from.
	Execute(context.Context, use.Arguments) (string, error)
}

// Map is the tools an exchange has available, looked up by name. It is what a
// provider lists in a request and what a [use.Type] coming back is resolved
// against.
//
// A registry is built once, by [New], and read from there on. Unlike the rest
// of the core it cannot enforce that — a map hands its keys to whoever holds
// it — so it is said here instead: writing to a registry after an exchange has
// been given it changes what the model was told it could ask for, out from
// under the request that told it.
type Map map[string]Type

// List contains the tools in the registry. A map has no order to hand back,
// so the order here is not one: a caller that needs the same sequence twice
// sorts what it gets. The slice is the caller's to scribble on — what comes
// back is a fresh one, and the registry is not reachable through it.
func (definitions Map) List() []Type {
	var list []Type
	for _, definition := range definitions {
		list = append(list, definition)
	}
	return list
}

// New gathers definitions into a set. Where two share a name the later one
// wins the lookup, since a set is what the caller assembled and the last word
// on a name is the one it meant.
func New(definitions ...Type) Map {
	m := make(map[string]Type, len(definitions))
	for _, definition := range definitions {
		name := definition.Name()
		m[name] = definition
	}
	return m
}
