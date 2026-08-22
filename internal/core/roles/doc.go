// Package roles contains the parts a message can take in a thread.
//
// A role is a plain string, owned by the core and spelled the core's way.
// Providers map these onto whatever their own API calls them; nothing here
// is a wire value, and no adapter is entitled to assume it is.
//
// There is no System role. The system prompt is not a turn in the exchange
// but a standing instruction on the thread, so it lives on the thread (see
// [github.com/ksahli/compadre/internal/core/threads.Type]) and each provider
// places it where its API wants it.
package roles
