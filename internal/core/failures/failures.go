package failures

import "errors"

// ErrSettled marks a failure that another attempt will not change. It is the
// difference between a turn that did not happen and a run that cannot go on:
// a rate limit or a reply cut off is the first, credentials the API will not
// accept is the second.
//
// Adapters wrap what they hand back with it, and only where they are sure —
// an unwrapped failure means the honest answer is that trying again might
// work, which is the answer worth defaulting to. A caller driving turns asks
// with [errors.Is] whether to come back to the prompt or to stop.
var ErrSettled = errors.New("asking again will not change the answer")
