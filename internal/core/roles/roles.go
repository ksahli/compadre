package roles

import (
	"errors"
	"fmt"
)

// ErrUnknown is what [Parse] reports of a name that is not one of the roles
// the core has. It is a sentinel so a caller reading a record can tell a turn
// filed under a word this core does not know from a database that would not
// answer at all.
var ErrUnknown = errors.New("the core has no such role")

// Type is the part a message takes in a thread. The set is closed: the field
// is unexported, so the only values are [User], [Assistant] and the zero
// Type, which is no role at all. Nothing outside this package can spell a
// third one, and a caller that has a Type has one the core knows.
//
// It is comparable, so a role is matched with == or switched on directly.
type Type struct {
	name string
}

var (
	// User is the part taken by whoever is driving the exchange.
	User = Type{name: "User"}
	// Assistant is the part taken by the model answering it.
	Assistant = Type{name: "Assistant"}
)

// String is how the role is spelled, which is what goes into the record. The
// zero Type has no name and stringifies to the empty string.
func (role Type) String() string {
	return role.name
}

// Parse turns a name back into the role it spells, and is the inverse of
// [Type.String]. The match is exact: a name the core has no role for is
// refused here rather than carried on as a turn nothing downstream can place.
func Parse(name string) (Type, error) {
	switch name {
	case User.name:
		return User, nil
	case Assistant.name:
		return Assistant, nil
	default:
		return Type{}, fmt.Errorf("%w: '%s'", ErrUnknown, name)
	}
}
