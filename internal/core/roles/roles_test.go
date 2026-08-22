package roles_test

import (
	"testing"

	"github.com/ksahli/compadre/internal/core/roles"
)

// TestValues pins the strings themselves, which reads as noise until you know
// what switches on them: every provider maps a role by comparing against these
// constants, so a changed value drops the messages carrying it rather than
// failing to compile. See the mapping in the anthropic adapter's parameters.
func TestValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"user", roles.User, "User"},
		{"assistant", roles.Assistant, "Assistant"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
			}
		})
	}
}
