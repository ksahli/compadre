// Package results contains the answer to a tool call.
//
// A result carries the id of the call it answers. The tool that ran has no
// way of knowing that id — it was handed arguments, not a call — so pairing
// the two is [github.com/ksahli/compadre/internal/core/tools.Invoke]'s job,
// and the id rides along here rather than being inferred later.
//
// Failure is a flag on the content, not an error return. A tool that blew up
// and a tool that answered are both things the model must be shown, and the
// content in the failed case is the explanation it reads before trying
// again. Nothing in this package treats a failure as an exit.
//
// [New] takes the flag rather than guessing at it. The two spellings a caller
// actually wants are
// [github.com/ksahli/compadre/internal/core/tools.Success] and
// [github.com/ksahli/compadre/internal/core/tools.Failure], which name the
// two cases and pull the content of a failure out of the error itself.
package results
