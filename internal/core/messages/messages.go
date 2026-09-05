package messages

import (
	"slices"

	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/tools/results"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/core/usage"
)

// Content is one piece of what a message says: something said, a tool the
// model asks for, or the answer to such a request. The set is closed — the
// unexported method means only this package can add a shape to it — and each
// shape answers true from its own accessor and false from the others, so a
// read of the wrong kind is never mistaken for real content.
type Content interface {
	Text() (string, bool)
	Use() (use.Type, bool)
	Result() (results.Type, bool)
	content()
}

// text is what the model or whoever drives the exchange actually says.
type text string

func (t text) Text() (string, bool)       { return string(t), true }
func (text) Use() (use.Type, bool)        { return use.Type{}, false }
func (text) Result() (results.Type, bool) { return results.Type{}, false }
func (text) content()                     {}

// toolUse is the model asking for a tool.
type toolUse struct {
	use use.Type
}

func (toolUse) Text() (string, bool)         { return "", false }
func (u toolUse) Use() (use.Type, bool)      { return u.use, true }
func (toolUse) Result() (results.Type, bool) { return results.Type{}, false }
func (toolUse) content()                     {}

// toolResult is the answer to a [Use], on its way back to the model.
type toolResult struct {
	result results.Type
}

func (toolResult) Text() (string, bool)           { return "", false }
func (toolResult) Use() (use.Type, bool)          { return use.Type{}, false }
func (r toolResult) Result() (results.Type, bool) { return r.result, true }
func (toolResult) content()                       {}

// Text is what is said, as itself.
func Text(said string) Content {
	return text(said)
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

// Type is a message: the part it takes in the exchange, and what it says, and
// what it cost. What it says is a list rather than a value, because one turn is
// often several things at once — a sentence and the tool call it leads to, or
// every result a round of calls produced.
type Type struct {
	role    roles.Type
	content []Content
	usage   usage.Type
}

func (message Type) Role() roles.Type {
	return message.role
}

// Usage is what the turn cost, where anybody counted it. A turn said to the
// model rather than by it was never metered and carries the zero count, which
// reports [github.com/ksahli/compadre/internal/core/usage.Type.Counted] false —
// so does a turn from a provider that does not account for itself. Reading the
// numbers without asking that first is reading a zero nobody measured.
func (message Type) Usage() usage.Type {
	return message.usage
}

// Content is what the message says, in order. The slice is the caller's to
// scribble on: what comes back is a copy.
func (message Type) Content() []Content {
	return slices.Clone(message.content)
}

// New builds a message from the part it takes and what it says. The content
// is copied, so a caller that keeps hold of the slice it passed in cannot
// reach back into the message with it. The role is a closed set — see
// [github.com/ksahli/compadre/internal/core/roles] — so the only turn that
// can be built without a part to take is one under the zero role, which is a
// turn nobody can place and which the providers refuse.
func New(role roles.Type, content ...Content) Type {
	message := Type{
		role:    role,
		content: slices.Clone(content),
	}
	return message
}

// With is the message under a count, and is how a provider that meters its
// turns says what one cost. It returns a successor rather than changing this
// one: a message is a value and what was said does not become something else
// because it was later added up. The content is copied again, so the two do
// not share a slice.
//
// It is on the message rather than a parameter of [New] because counting is
// the exception. Every turn has a part to take and something to say; only the
// ones a model wrote come back with a price on them.
func (message Type) With(count usage.Type) Type {
	message.content = slices.Clone(message.content)
	message.usage = count
	return message
}
