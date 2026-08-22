package results

// Type represents the answer to a [Use]. It carries the id of the call it answers,
// what the model should be shown, and whether that content is an explanation
// of a failure rather than an answer.
type Type struct {
	id      string
	content string
	failed  bool
}

func (result Type) ID() string {
	return result.id
}

func (result Type) Content() string {
	return result.content
}

func (result Type) Failed() bool {
	return result.failed
}

func New(id, content string, failed bool) Type {
	result := Type{
		id:      id,
		content: content,
		failed:  failed,
	}
	return result
}
