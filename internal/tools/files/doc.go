// Package files lets the model see what is in the workspace.
//
// It implements
// [github.com/ksahli/compadre/internal/core/tools/definitions.Type], and it
// sits here rather than in core for the same reason a provider does: the core
// says what a tool is, this package is one. A tool that reaches a filesystem
// is an adapter onto the world, no differently from one that reaches a
// service, and the dependency points the one way the rest of the tree points.
//
// The whole of this package is one bound. Listing a directory is the easy
// half; the half worth writing down is that the answer is about one rooted
// workspace and cannot be argued out of it. Three ways out are refused by
// name: an absolute path, a relative one that climbs past the root with '..',
// and a symlink whose target is elsewhere. The first two are lexical and
// [path/filepath.Join] settles them; the third is not, which is why every path
// is put through [path/filepath.EvalSymlinks] before it is judged and the
// judgement is a [path/filepath.Rel] against a root that was itself resolved.
// Comparing strings before resolving them would pass a link that points away.
//
// The root is a field on [Tool], set by [New], rather than something read from
// the process at call time. That is the seam the weather tool has in its
// endpoints: a test hands this one a [testing.T.TempDir] and the tool never
// touches the tree it is running in. It is also the honest shape — the
// workspace is a decision the wiring makes once, and a tool that read the
// working directory itself would answer differently depending on when it was
// asked. There is no option and no default, because unlike an endpoint a root
// has nothing sensible to fall back to: a tool built with the wrong one is not
// degraded, it is pointed at somebody else's files.
//
// A path leading out is an error the model reads, not a path quietly clamped
// to the root. Clamping would answer a question nobody asked and give the
// model no way to tell that it had; the error names the value that was wrong,
// and the model can ask for somewhere else on the next turn. That is the same
// instinct as [github.com/ksahli/compadre/internal/core/tools.Invoke] handing
// back a failed result rather than stopping the program.
//
// Two limits are policy rather than mechanism, and are worth saying out loud.
// The contents of .git are never walked — the directory itself is listed, so
// the model can see it is there, but a walk does not enter it: it holds
// thousands of files that would spend the whole ceiling before the walk
// reached anything asked about.
// And the ceiling itself, maxEntries, is the same instinct as the weather
// tool's ceiling on a response body — how large a tree this process is pointed
// at is not its call. A listing that hit the ceiling says so, because a
// partial answer passed off as a whole one is worse than a short one.
//
// A recursive listing does not follow symlinks. [path/filepath.WalkDir] does
// not, and that is wanted twice over: a link out of the workspace is reported
// as an entry rather than traversed, and a link back into the tree cannot make
// the walk loop.
//
// What [Tool.Execute] returns is prose, not JSON, for the reason every tool
// here gives: the reader is a model, and a sentence costs it less than a shape
// it has to decode before it can read.
package files
