package memory

import (
	"context"

	"github.com/ksahli/compadre/internal/core/exchanges"
)

type (
	Context  = context.Context
	Exchange = exchanges.Type
)

// Store is the port an exchange is kept through: two methods, no engine
// named. Implementations live in stores and are the only code that knows a
// database exists.
type Store interface {
	// Save writes the exchange as it now stands and returns it filed under
	// the id it was given. An exchange with no id yet is opened and given
	// one, which is why an exchange comes back rather than nothing: the id
	// is the store's to mint and the caller has no other way to learn it.
	//
	// An exchange is only ever appended to, so a store may write the turns
	// it has not seen before rather than the whole of it again.
	Save(Context, Exchange) (Exchange, error)
	// Load returns the exchange filed under an id. An id nothing was filed
	// under is an error: an empty exchange and a missing one are different
	// answers and must not arrive as the same one.
	Load(Context, string) (Exchange, error)
}
