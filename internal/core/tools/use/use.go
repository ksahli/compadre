package use

import (
	"encoding/json"
)

type Arguments = json.RawMessage

// Type represents the model asking for a tool: which one, with what arguments, and
// the id the answer has to carry back so the two can be paired.
type Type struct {
	id        string
	name      string
	arguments Arguments
}

func (use Type) ID() string {
	return use.id
}

func (use Type) Name() string {
	return use.name
}

func (use Type) Arguments() Arguments {
	return use.arguments
}

// New builds the model's request to run a tool. The input is taken as it
// arrived and is not validated here: whether it parses is the tool's business.
func New(id, name string, arguments Arguments) Type {
	return Type{
		id:        id,
		name:      name,
		arguments: arguments,
	}
}
