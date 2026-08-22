# compadre

An agent runtime in Go, built ports first: the core owns the vocabulary — roles,
messages, threads — and reaches a model through a single interface. No model
vendor is named anywhere inside it. Providers are adapters at the edge, and are
the only code that knows an SDK exists.

## Status

Early. What works today:

- a thread of messages, the system prompt among them, left to each provider to
  place where its API wants it
- an `inference.Provider` port: give it a thread and the tools the model may
  ask for, get its replies back whole
- an Anthropic adapter over the Messages API, mapping turns in both directions
  — what was said, a tool the model asks for, and the answer to one
- tool use end to end: the `invoke` command gathers a registry, hands it to the
  provider, runs whatever the model asks for and sends the results back, until
  the model has nothing left to ask for
- a `weather` tool over Open-Meteo, the first thing in the tree to implement
  the core's tool port, and reachable by the model
- `list_files`, `read_file` and `write_file` tools over the directory the
  command was run in, confined to it: an absolute path, a `..` climbing past
  the root and a symlink pointing away are each refused rather than answered
- unit tests over the core packages, the anthropic adapter and the loop

What does not exist yet: streaming, so a reply arrives whole rather than as it
is written; any way to choose the model, the token ceiling or the tools from
the command line; and any memory of an exchange once the command exits.

## Layout

```
commands                 the command line, one package per command
internal/core/roles      the parts a message can take
internal/core/messages   message primitives
internal/core/threads    the exchange a provider is asked to continue
internal/core/inference  the port a model is reached through
internal/core/tools      what a tool is, in the core's own terms
internal/providers/…     adapters implementing that port
internal/tools/…         tools that are, one package each
```

Dependencies point one way: `providers` and `tools` know `core`, never the
reverse, and `core` names no vendor or service.

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
