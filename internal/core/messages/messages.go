package messages

import (
	"slices"

	"github.com/ksahli/compadre/internal/core/thoughts"
	"github.com/ksahli/compadre/internal/core/tools/results"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

// Content is one piece of what a message says: something said, the reasoning
// behind it, a tool the model asks for, or the answer to such a request. The
// set is closed — the unexported method means only this package can add a
// shape to it — and each shape answers true from its own accessor and false
// from the others, so a read of the wrong kind is never mistaken for real
// content.
type Content interface {
	Text() (string, bool)
	Thought() (thoughts.Type, bool)
	Use() (use.Type, bool)
	Result() (results.Type, bool)
	content()
}

// text is what the model or whoever drives the exchange actually says.
type text string

func (t text) Text() (string, bool)         { return string(t), true }
func (text) Thought() (thoughts.Type, bool) { return thoughts.Type{}, false }
func (text) Use() (use.Type, bool)          { return use.Type{}, false }
func (text) Result() (results.Type, bool)   { return results.Type{}, false }
func (text) content()                       {}

// thought is the model's reasoning on its way back to the model. The core
// never reads one; it keeps it because the turn it belongs to is not whole
// without it.
type thought struct {
	thought thoughts.Type
}

func (thought) Text() (string, bool)             { return "", false }
func (t thought) Thought() (thoughts.Type, bool) { return t.thought, true }
func (thought) Use() (use.Type, bool)            { return use.Type{}, false }
func (thought) Result() (results.Type, bool)     { return results.Type{}, false }
func (thought) content()                         {}

// toolUse is the model asking for a tool.
type toolUse struct {
	use use.Type
}

func (toolUse) Text() (string, bool)           { return "", false }
func (toolUse) Thought() (thoughts.Type, bool) { return thoughts.Type{}, false }
func (u toolUse) Use() (use.Type, bool)        { return u.use, true }
func (toolUse) Result() (results.Type, bool)   { return results.Type{}, false }
func (toolUse) content()                       {}

// toolResult is the answer to a [Use], on its way back to the model.
type toolResult struct {
	result results.Type
}

func (toolResult) Text() (string, bool)           { return "", false }
func (toolResult) Thought() (thoughts.Type, bool) { return thoughts.Type{}, false }
func (toolResult) Use() (use.Type, bool)          { return use.Type{}, false }
func (r toolResult) Result() (results.Type, bool) { return r.result, true }
func (toolResult) content()                       {}

// Text is what is said, as itself.
func Text(said string) Content {
	return text(said)
}

// Thinking is the reasoning that led to what a message says, carried whole.
// It is content like any other because that is what it is to the API that
// produced it: part of the turn, and part of what has to go back for the turn
// to continue.
func Thinking(reasoning thoughts.Type) Content {
	return thought{thought: reasoning}
}

// Use is a request to run a tool, carried whole: the id the answer has to
// come back with is part of it, so a [Result] can be paired with it later.
func Use(request use.Type) Content {
	return toolUse{use: request}
}

// Result is the answer to a request, whether it ran or failed. It is content
// like any other: the model reads it as the next thing said to it.
func Result(answer results.Type) Content {
	return toolResult{result: answer}
}

// Type is a message: the part it takes in the exchange, and what it says.
// What it says is a list rather than a value, because one turn is often
// several things at once — a sentence and the tool call it leads to, or every
// result a round of calls produced.
type Type struct {
	role    string
	content []Content
}

func (message Type) Role() string {
	return message.role
}

// Content is what the message says, in order. The slice is the caller's to
// scribble on: what comes back is a copy.
func (message Type) Content() []Content {
	return slices.Clone(message.content)
}

// New builds a message from the part it takes and what it says. The content
// is copied, so a caller that keeps hold of the slice it passed in cannot
// reach back into the message with it. The role is not constrained: the
// constants in [github.com/ksahli/compadre/internal/core/roles] are what the
// core spells its own roles with, not a closed set.
func New(role string, content ...Content) Type {
	message := Type{
		role:    role,
		content: slices.Clone(content),
	}
	return message
}
