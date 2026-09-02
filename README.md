# compadre

An agent runtime in Go, built ports first: the core owns the vocabulary — roles,
messages, threads — and reaches a model through one interface and its record
through another. No model vendor and no storage engine is named anywhere inside
it. Providers and stores are adapters at the edge, and are the only code that
knows an SDK or a database exists.

## Status

Early. What works today:

- a thread of messages, the system prompt among them, left to each provider to
  place where its API wants it
- an `inference.Provider` port: give it a thread and the tools the model may
  ask for, get its replies back whole
- an Anthropic adapter over the Messages API, mapping turns in both directions
  — what was said, a tool the model asks for, and the answer to one
- tool use end to end: an `agents` package in the core runs the exchange —
  whatever the model asks for is run and answered, and the exchange goes on
  until it has nothing left to ask for; the `invoke` command gathers the
  registry, hands it over, and prints what was said
- a `weather` tool over Open-Meteo, the first thing in the tree to implement
  the core's tool port, and reachable by the model
- `list_files`, `read_file` and `write_file` tools over the directory the
  command was run in, confined to it: an absolute path, a `..` climbing past
  the root and a symlink pointing away are each refused rather than answered
- a `http_get` tool that fetches a page and hands back the text of it, over
  https only and only to addresses on the open internet
- a record of every exchange, kept in SQLite as it happens: a `memory.Store`
  port in the core and an adapter behind it, written to after every turn rather
  than once at the end, so a run that was refused or interrupted still left the
  turns that got that far
- unit tests over the core packages, the anthropic adapter, the loop and the
  store

What does not exist yet: streaming, so an exchange arrives whole when it ends
rather than a word or a turn at a time — a run in which the model calls several
tools is silent until the last of them is done; any way to choose the model, the
token ceiling or the tools from the command line; and any way to pick a stored
exchange back up from the command line, though the store can already read one
back.

## Layout

```
commands                 the command line, one package per command
internal/core/roles      the parts a message can take
internal/core/messages   message primitives
internal/core/threads    the exchange a provider is asked to continue
internal/core/inference  the port a model is reached through
internal/core/exchanges  a thread and the id it is filed under
internal/core/memory     the port an exchange is kept through
internal/core/tools      what a tool is, in the core's own terms
internal/core/agents     the loop that runs an exchange to its end
internal/providers/…     adapters implementing the inference port
internal/stores/…        adapters implementing the memory port
internal/tools/…         tools that are, one package each
```

Dependencies point one way: `providers`, `stores` and `tools` know `core`, never
the reverse, and `core` names no vendor, service or engine.

## Usage

```sh
export ANTHROPIC_API_KEY=…
go run . invoke -message 'why is the sky blue?'
```

`invoke` also takes `-instructions` for system instructions:

```sh
go run . invoke -instructions 'answer in one sentence' -message 'why is the sky blue?'
```

The `weather` tool is offered on every invocation, so a question about the
weather somewhere is answered by calling it:

```sh
go run . invoke -message 'what is the weather in Paris right now?'
```

The `http_get` tool is offered the same way, for reading a page the model is
pointed at:

```sh
go run . invoke -message 'read https://go.dev/doc/effective_go and summarise the section on errors'
```

The `list_files`, `read_file` and `write_file` tools are offered the same way,
over the directory `compadre` was run in:

```sh
go run . invoke -message 'list the go files in this project and tell me what it does'
go run . invoke -message 'read internal/core/tools/tools.go and explain Invoke'
go run . invoke -message 'write a hello.go that prints hello'
```

Note the third: `invoke` can now change the directory it was run in. It can
change nothing else. Paths are relative to that directory, and one leading
outside is refused by name rather than quietly answered about — or written
into — the root. `.git` is listed but never walked into, never read out of and
never written to.

`write_file` creates and does not replace. A path that already exists is
refused rather than overwritten, so a file the model wants to change is a file
it has to write under a new name; directories on the way that do not exist are
created. Content is capped at 256 KiB, and content that is not text is refused.

The reading tools admit when an answer is short of the whole thing. A listing
is cut off at a thousand entries with a line saying so. A read is cut off at
two thousand lines or 256 KiB, and says which line to ask again from —
`read_file` takes `offset` and `limit`, so a long file is read a window at a
time. A file that is not text is refused rather than spent on the model's
context.

Every exchange is written down as it happens, into a SQLite database under the
user's config directory — `~/.config/compadre/exchanges.db` on Linux. `-store`
points somewhere else:

```sh
go run . invoke -store ./exchanges.db -message 'why is the sky blue?'
```

The id the exchange was filed under is printed to stderr when the run ends, so
stdout stays what the model said and nothing else. The record is written after
every turn rather than once at the end, which means a run that was refused
half way through, or interrupted, still left behind the turns that got that
far. Its three tables are the vocabulary itself — a thread, its turns, and the
blocks of each turn — so what the model reached for is a question the record
can answer:

```sh
sqlite3 ~/.config/compadre/exchanges.db \
  "SELECT tool, count(*) FROM contents WHERE kind = 'use' GROUP BY tool"
```

No command reads one back yet — the store can, but nothing on the command line
asks it to. Picking an exchange up and continuing it is the next thing.

`compadre help` lists the commands, and `compadre invoke -h` the arguments
`invoke` takes.

The model and token ceiling are fixed in the Anthropic adapter for now, and the
tools on offer are fixed in the `invoke` command; none is reachable from the
command line. An exchange is bounded at ten turns, so a model that keeps asking
for tools is stopped rather than left to spend. A reply the model stopped short
of finishing — cut off at the token ceiling, declined, or out of context — is an
error rather than a half answer passed off as a whole one, and an interrupt
cancels the exchange rather than killing the process mid-request.

## Development

```sh
go build ./... && go vet ./... && go test ./...
```

CI runs the same three, plus `gofmt -l` and `govulncheck`.

## License

MIT. See [LICENSE](LICENSE).
