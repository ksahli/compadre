package use_test

import (
	"testing"

	"github.com/ksahli/compadre/internal/core/tools/use"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name      string
		use       use.Type
		id        string
		tool      string
		arguments string
	}{
		{
			name:      "a call with arguments",
			use:       use.New("call_1", "read_file", use.Arguments(`{"path":"main.go"}`)),
			id:        "call_1",
			tool:      "read_file",
			arguments: `{"path":"main.go"}`,
		},
		{
			// A tool that takes nothing is called with nothing.
			name:      "a call without arguments",
			use:       use.New("call_2", "pwd", nil),
			id:        "call_2",
			tool:      "pwd",
			arguments: "",
		},
		{
			// Arguments that do not parse are stored as they
			// arrived. Whether they parse is the tool's business,
			// and a tool that cannot read them says so in a failed
			// result the model can act on.
			name:      "arguments that are not valid JSON",
			use:       use.New("call_3", "read_file", use.Arguments(`{"path":`)),
			id:        "call_3",
			tool:      "read_file",
			arguments: `{"path":`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.use.ID(); got != c.id {
				t.Errorf("Type.ID() = %q, want %q", got, c.id)
			}
			if got := c.use.Name(); got != c.tool {
				t.Errorf("Type.Name() = %q, want %q", got, c.tool)
			}
			if got := string(c.use.Arguments()); got != c.arguments {
				t.Errorf("Type.Arguments() = %q, want %q", got, c.arguments)
			}
		})
	}
}
