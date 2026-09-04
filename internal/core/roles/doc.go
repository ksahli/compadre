// Package roles contains the parts a message can take in a thread.
//
// A role is a closed set owned by the core and spelled the core's way: the
// only values are [User], [Assistant] and the zero [Type], which is no role
// at all. Providers map these onto whatever their own API calls them; nothing
// here is a wire value, and no adapter is entitled to assume it is.
//
// Text becomes a role only through [Parse], which is for the one place a role
// legitimately arrives as a word rather than a value — a turn read back out of
// a record — and which refuses a word the core has no role for there, at the
// edge it came in by.
//
// There is no System role. The system prompt is not a turn in the exchange
// but a standing instruction on the thread, so it lives on the thread (see
// [github.com/ksahli/compadre/internal/core/threads.Type]) and each provider
// places it where its API wants it.
package roles
