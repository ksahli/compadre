package anthropic

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ksahli/compadre/internal/core/inference"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/use"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type (
	Context = context.Context
	// Option configures the underlying client. It is the seam for
	// pointing the adapter somewhere other than the real API — a test
	// server, a proxy — without the core knowing such a thing exists.
	Option = option.RequestOption
)

// blocks maps what a message says onto the content blocks the API expects,
// in the order it said them. Arguments go out as they arrived: they are the
// model's own JSON, and re-encoding them would be this adapter putting words
// in its mouth.
func blocks(message messages.Type) []sdk.ContentBlockParamUnion {
	blocks := []sdk.ContentBlockParamUnion{}
	for _, content := range message.Content() {
		if text, ok := content.Text(); ok {
			blocks = append(blocks, sdk.NewTextBlock(text))
		}
		if use, ok := content.Use(); ok {
			blocks = append(blocks, sdk.NewToolUseBlock(use.ID(), use.Arguments(), use.Name()))
		}
		if result, ok := content.Result(); ok {
			blocks = append(blocks, sdk.NewToolResultBlock(result.ID(), result.Content(), result.Failed()))
		}
	}
	return blocks
}

// schema unpacks the plain JSON Schema a definition hands over onto the shape
// this API wants. The core deals in a map because it is data on its way to a
// wire format it is not entitled to know; this is where it acquires one. A key
// that is missing, or not the shape it should be, is left at its zero value
// rather than guessed at — the same refusal as [blocks] and [reply].
func schema(definition map[string]any) sdk.ToolInputSchemaParam {
	input := sdk.ToolInputSchemaParam{
		Properties: definition["properties"],
	}

	switch required := definition["required"].(type) {
	case []string:
		input.Required = slices.Clone(required)
	case []any:
		for _, name := range required {
			if name, ok := name.(string); ok {
				input.Required = append(input.Required, name)
			}
		}
	}

	return input
}

// catalogue maps the registry onto the tools the request advertises. The
// registry is a map and has no order to hand back, so the list is sorted by
// name: a request built twice from the same set should be the same request.
// An empty registry produces nothing rather than an empty list, because
// "these are the tools" and "there are none" are different things to say.
func catalogue(registry tools.Registry) []sdk.ToolUnionParam {
	list := registry.List()
	slices.SortFunc(list, func(a, b tools.Definition) int {
		return strings.Compare(a.Name(), b.Name())
	})

	catalogue := []sdk.ToolUnionParam{}
	for _, definition := range list {
		tool := sdk.ToolParam{
			Name:        definition.Name(),
			Description: sdk.String(definition.Description()),
			InputSchema: schema(definition.Schema()),
		}
		catalogue = append(catalogue, sdk.ToolUnionParam{OfTool: &tool})
	}

	if len(catalogue) == 0 {
		return nil
	}

	return catalogue
}

// parameters maps a thread and the tools on offer onto the request the API
// expects. Roles become SDK messages; the thread's instructions become the
// system prompt, which is where this API wants them. A role the core has that
// this mapping does not cover is skipped rather than guessed at, and so is a
// message that turns out to say nothing this adapter has a block for.
func parameters(thread threads.Type, registry tools.Registry) sdk.MessageNewParams {
	turns := []sdk.MessageParam{}
	for _, message := range thread.Messages() {
		content := blocks(message)
		if len(content) == 0 {
			continue
		}
		switch message.Role() {
		case roles.User:
			turns = append(turns, sdk.NewUserMessage(content...))
		case roles.Assistant:
			turns = append(turns, sdk.NewAssistantMessage(content...))
		}
	}

	parameters := sdk.MessageNewParams{
		Model:     sdk.ModelClaudeSonnet5,
		MaxTokens: 1024,
		Messages:  turns,
		Tools:     catalogue(registry),
	}

	if len(thread.Instructions()) > 0 {
		parameters.System = []sdk.TextBlockParam{
			{Text: thread.Instructions()},
		}
	}

	return parameters
}

// provider is the adapter itself. It is unexported because callers hold the
// port, not this type; [New] is the only way to get one.
type provider struct {
	client sdk.Client
}

// reply turns the response's content blocks into one assistant message
// carrying all of them, in the order they arrived. Text arrives as itself; a
// tool call arrives whole — id, name and the arguments as the model sent
// them, which is what pairs the answer with the call later. A block this
// mapping has no shape for is skipped rather than guessed at, and a response
// with nothing left after that is no message at all — which [provider.Invoke]
// reports as an error, since it is the one holding an error return.
func reply(response *sdk.Message) []messages.Type {
	content := []messages.Content{}
	for _, block := range response.Content {
		if text, ok := block.AsAny().(sdk.TextBlock); ok {
			content = append(content, messages.Text(text.Text))
		}
		if tool, ok := block.AsAny().(sdk.ToolUseBlock); ok {
			// Raw() is the argument JSON as sent, which is what
			// the tool is owed. Marshalling tool.Input would be
			// this adapter's rendering of it instead.
			arguments := use.Arguments(tool.JSON.Input.Raw())
			content = append(content, messages.Use(use.New(tool.ID, tool.Name, arguments)))
		}
	}
	if len(content) == 0 {
		return []messages.Type{}
	}
	return []messages.Type{messages.New(roles.Assistant, content...)}
}

// refused reports why a response is not an answer, or nil where it is one.
// The API says how the model came to stop, and three of those reasons mean
// what came back is not a reply the caller can act on: a turn cut off at the
// ceiling is half a sentence, and half a tool call is worse — arguments that
// stop mid-JSON go on to fail in the tool. The core has no vocabulary for a
// stop reason and should not grow one, so the reason is read here and spent
// here, as the error the port already allows.
func refused(response *sdk.Message) error {
	switch response.StopReason {
	case sdk.StopReasonMaxTokens:
		return errors.New("the model's reply was cut off at the token ceiling")
	case sdk.StopReasonRefusal:
		return errors.New("the model declined to answer")
	case sdk.StopReasonModelContextWindowExceeded:
		return errors.New("the exchange no longer fits in the model's context window")
	default:
		return nil
	}
}

// Invoke implements [inference.Provider]. One round trip: the thread and the
// tools on offer go out as request parameters, and the reply comes back whole
// — including a request to run one of those tools, which is content like any
// other and the caller's to act on. A request the API refuses comes back as an
// error rather than as an empty reply, and so does a response that is no reply:
// one the model stopped short of finishing, and one carrying nothing this
// adapter has a shape for. Ending an exchange silently and ending it well look
// the same to a caller, which is reason enough not to let the first happen.
func (p *provider) Invoke(ctx Context, thread threads.Type, registry tools.Registry) ([]messages.Type, error) {
	response, err := p.client.Messages.New(ctx, parameters(thread, registry))
	if err != nil {
		return nil, err
	}
	if err := refused(response); err != nil {
		return nil, err
	}

	replies := reply(response)
	if len(replies) == 0 {
		return nil, fmt.Errorf("the model answered with nothing to read (stopped: %s)", response.StopReason)
	}

	return replies, nil
}

// New builds an adapter over the Anthropic Messages API. It returns the
// port rather than a concrete type: whoever calls this is the wiring, and
// everything downstream of it should see only [inference.Provider].
//
// Credentials come from the environment unless an [Option] says otherwise.
func New(options ...Option) inference.Provider {
	return &provider{
		client: sdk.NewClient(options...),
	}
}
