// Package files lets the model see what is in the workspace: [List] names what
// is there, [Read] shows what is in one of them.
//
// Both implement
// [github.com/ksahli/compadre/internal/core/tools/definitions.Type], and they
// sit here rather than in core for the same reason a provider does: the core
// says what a tool is, this package is two of them. A tool that reaches a
// filesystem is an adapter onto the world, no differently from one that
// reaches a service, and the dependency points the one way the rest of the
// tree points.
//
// The whole of this package is one bound. Listing a directory and reading a
// file are the easy halves; the half worth writing down is that both answers
// are about one rooted workspace and cannot be argued out of it. That is why
// there are two tools in one package and not two packages: the bound is the
// thing, it lives once on the unexported workspace type, and a second copy of
// a containment check is a second chance to get it wrong.
//
// Three ways out are refused by name: an absolute path, a relative one that
// climbs past the root with '..', and a symlink whose target is elsewhere. The
// first two are lexical and [path/filepath.Join] settles them; the third is
// not, which is why every path is put through [path/filepath.EvalSymlinks]
// before it is judged and the judgement is a [path/filepath.Rel] against a
// root that was itself resolved. Comparing strings before resolving them would
// pass a link that points away. Every later judgement — whether a path is a
// directory, whether it is inside .git — is made on the resolved path for the
// same reason.
//
// The root is a field on the workspace, set by [NewList] and [NewRead], rather
// than something read from the process at call time. That is the seam the
// weather tool has in its endpoints: a test hands these a
// [testing.T.TempDir] and neither tool touches the tree it is running in. It
// is also the honest shape — the workspace is a decision the wiring makes
// once, and a tool that read the working directory itself would answer
// differently depending on when it was asked. There is no option and no
// default, because unlike an endpoint a root has nothing sensible to fall back
// to: a tool built with the wrong one is not degraded, it is pointed at
// somebody else's files.
//
// A path leading out is an error the model reads, not a path quietly clamped
// to the root. Clamping would answer a question nobody asked and give the
// model no way to tell that it had; the error names the value that was wrong,
// and the model can ask for somewhere else on the next turn. That is the same
// instinct as [github.com/ksahli/compadre/internal/core/tools.Invoke] handing
// back a failed result rather than stopping the program.
//
// Some limits are policy rather than mechanism, and are worth saying out loud.
//
// .git is inside the workspace and is treated as neither in nor out. [List]
// names the directory, so the model can see it is there, but does not walk
// into it: it holds thousands of files that would spend the whole ceiling
// before the walk reached anything asked about. [Read] refuses anything inside
// it outright — no question about what a project is or does is answered by a
// packfile, and a tool that will not list those paths has no business handing
// one back when asked for it directly.
//
// Each tool has a ceiling, and they are the same instinct as the weather
// tool's ceiling on a response body: how large a tree or a file this process
// was pointed at is not its call. maxEntries bounds a listing; maxLines and
// maxBytes bound a read, the second of them because a file can be one line.
// An answer that hit a ceiling says so, because a partial answer passed off as
// a whole one is worse than a short one — and a read cut off by maxBytes does
// not report how long the file is at all, since the number it has is a floor
// and reporting it would be a wrong answer rather than a missing one.
//
// [Read] answers in a window rather than only from the start, which is what
// makes a ceiling something the model can get past: a read that stopped short
// names the line to ask again from, the counterpart of a listing telling it to
// ask about a directory further in. A line offset is not a byte offset, so the
// file is walked rather than seeked into — there is no finding the one from
// the other without reading what lies between.
//
// A file that is not text is refused rather than shown. Bytes that do not
// decode cost the model context and tell it nothing, and being told plainly
// that a file is not readable is worth more than being handed the inside of a
// PNG.
//
// A recursive listing does not follow symlinks. [path/filepath.WalkDir] does
// not, and that is wanted twice over: a link out of the workspace is reported
// as an entry rather than traversed, and a link back into the tree cannot make
// the walk loop. A link is still a path a read can be pointed at, and one
// landing inside the workspace is read: the bound is about where a path ends
// up, not about how it was spelled.
//
// What Execute returns is prose, not JSON, for the reason every tool here
// gives: the reader is a model, and a sentence costs it less than a shape it
// has to decode before it can read.
package files
