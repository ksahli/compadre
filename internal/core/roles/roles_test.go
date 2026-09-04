package roles_test

import (
	"errors"
	"testing"

	"github.com/ksahli/compadre/internal/core/roles"
)

// TestString pins the spellings themselves, which reads as noise until you
// know what holds them: a role goes into the record as its name and comes back
// out through [roles.Parse], so a changed spelling makes every exchange
// already on disk unreadable — a failure at run time, on a real record, rather
// than at compile time.
func TestString(t *testing.T) {
	cases := []struct {
		name string
		got  roles.Type
		want string
	}{
		{"user", roles.User, "User"},
		{"assistant", roles.Assistant, "Assistant"},
		// The zero role is nobody's part, and has no name to be spelled.
		{"the zero role", roles.Type{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.got.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestParse covers the other direction, which is the one the record depends
// on: every name a role is written under has to come back as that role.
func TestParse(t *testing.T) {
	cases := []struct {
		name string
		want roles.Type
	}{
		{"user", roles.User},
		{"assistant", roles.Assistant},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := roles.Parse(c.want.String())
			if err != nil {
				t.Fatalf("Parse(%q) = _, %v, want no error", c.want, err)
			}
			if got != c.want {
				t.Errorf("Parse(%q) = %q, want %q", c.want, got, c.want)
			}
		})
	}
}

// TestParseUnknown covers what a record written by something else holds: a
// word the core has no role for. It is refused, and refused with a sentinel,
// so a caller can tell it from a database that would not answer at all.
func TestParseUnknown(t *testing.T) {
	cases := []struct {
		name string
		part string
	}{
		{"a role the core does not have", "Stranger"},
		// Parse is the inverse of String, and String is exact.
		{"the right role spelled wrong", "user"},
		{"nothing at all", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := roles.Parse(c.part)
			if !errors.Is(err, roles.ErrUnknown) {
				t.Fatalf("Parse(%q) = _, %v, want %v", c.part, err, roles.ErrUnknown)
			}
			if got != (roles.Type{}) {
				t.Errorf("Parse(%q) = %q, want no role", c.part, got)
			}
		})
	}
}
