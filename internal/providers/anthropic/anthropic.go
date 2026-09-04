package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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

// Model is the model an adapter reaches when it is built without one named,
// and Tokens the ceiling on one reply when it is built without one. They are
// stated here rather than at the wiring because which model this vendor has
// and what a sensible ceiling on it is are facts about this API, and this is
// the package entitled to know them.
const (
	Model        = string(sdk.ModelClaudeSonnet5)
	Tokens int64 = 1024
)

// The ways an exchange can end without an answer: what the API said when it
// would not take the request, and what the model said when it stopped short of
// a reply. They are one vocabulary because they are one thing to the caller —
// a turn that did not happen — and they are exported so that a caller who
// wants to tell one from another can, with [errors.Is], rather than by reading
// this package's own words back.
//
// The API's account of itself — its status line, its error body, the request
// id — is read here and not repeated. What the person at the terminal needs is
// what became of their run, not what the wire said about it.
var (
	ErrCredentials = errors.New("the API would not accept the credentials")
	ErrPermission  = errors.New("the credentials are not allowed to ask this")
	ErrBilling     = errors.New("the account cannot be billed for the request")
	ErrModel       = errors.New("the API does not know the model it was asked for")
	ErrRequest     = errors.New("the API would not take the request as it was built")
	ErrTooLarge    = errors.New("the request was larger than the API will take")
	ErrRateLimited = errors.New("the API is turning requests away for now")
	ErrUnavailable = errors.New("the API could not answer the request")
	ErrRefused     = errors.New("the API refused the request")

	ErrCutOff   = errors.New("the model's reply was cut off at the token ceiling")
	ErrDeclined = errors.New("the model declined to answer")
	ErrContext  = errors.New("the exchange no longer fits in the model's context window")
)

// blocks maps what a message says onto the content blocks the API expects,
// in the order it said them. Arguments go out as they arrived, because they
// are the model's own and re-encoding them would be this adapter putting words
// in its mouth. A turn read out of the record can hold no reasoning, since
// [reply] keeps none, so there is nothing of the kind here to send back.
func blocks(message messages.Type) []sdk.ContentBlockParamUnion {
	blocks := []sdk.ContentBlockParamUnion{}
	for _, content := range message.Content() {
		// An empty one is dropped rather than sent. The API will not
		// take a text block with nothing in it, and a turn already on
		// disk carrying one — written before [reply] stopped keeping
		// them — would otherwise make every later request in that
		// exchange unsendable. A message left with no blocks at all is
		// skipped by [provider.parameters], which is the right answer
		// for a turn that said nothing.
		if text, ok := content.Text(); ok && text != "" {
			blocks = append(blocks, sdk.NewTextBlock(text))
		}
		if use, ok := content.Use(); ok {
			blocks = append(blocks, sdk.NewToolUseBlock(use.ID(), use.Arguments(), use.Name()))
		}
		if result, ok := content.Result(); ok {
			// A result the API will take, whatever the tool said.
			// The SDK wraps the content in a text block, so an
			// answer of nothing would be the empty block the API
			// refuses — and dropping it is not the way out of
			// that, since a call left with no answer is a turn no
			// provider will continue. So the absence is said in
			// words instead. Whether it was an answer or a failure
			// is carried by the flag, as it always was.
			said := result.Content()
			if said == "" {
				said = "the tool returned nothing"
			}
			blocks = append(blocks, sdk.NewToolResultBlock(result.ID(), said, result.Failed()))
		}
	}
	return blocks
}

// schema unpacks the plain JSON Schema a definition hands over onto the shape
// this API wants. The core deals in a map because it is data on its way to a
// wire format it is not entitled to know; this is where it acquires one. A key
// that is missing, or not the shape it should be, is left at its zero value
// rather than guessed at — the same refusal as [blocks] and [reply].
//
// Two keys have fields here and the rest have none, which is not a reason to
// leave them behind: a definition that wrote additionalProperties, or a $defs
// its properties point at, wrote a contract, and sending a weaker one than the
// tool declared is this adapter deciding what the tool meant. So they go out
// as they came in. Only type is dropped, and only because the SDK spells it
// itself and will not be told otherwise.
func schema(definition map[string]any) sdk.ToolInputSchemaParam {
	input := sdk.ToolInputSchemaParam{
		Properties: definition["properties"],
	}

	for key, value := range definition {
		switch key {
		case "type", "properties", "required":
			continue
		}
		if input.ExtraFields == nil {
			input.ExtraFields = map[string]any{}
		}
		input.ExtraFields[key] = value
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
// system prompt, which is where this API wants them. A message that turns out
// to say nothing this adapter has a block for is skipped, because a turn that
// said nothing is nothing to send.
//
// A turn with no role this mapping covers is refused instead. The roles the
// core has are a closed set and both have a part here, so what reaches the
// default arm is the zero role: a turn built without a part to take. Leaving
// it out would send a conversation missing a turn that is sitting in the
// record, which is a worse answer than saying so. It is the same refusal
// [provider.Invoke] makes of a response that is no reply.
//
// The two are settled in that order, and the order is the whole of it. Whether
// a turn can be placed at all is a question about the record; whether it said
// anything is a question about its contents. Asking the second one first lets
// the skip swallow the refusal: a turn nobody can place would go quietly
// whenever it happened to leave [blocks] with nothing, and that is not only
// the empty turn, since [blocks] has arms for four kinds of content and drops
// whatever it cannot map.
//
// It is a method rather than a function because the model and the ceiling are
// what the adapter was built with, not something a thread carries: the core
// has no vocabulary for either and should not grow one.
func (p *provider) parameters(thread threads.Type, registry tools.Registry) (sdk.MessageNewParams, error) {
	turns := []sdk.MessageParam{}
	for _, message := range thread.Messages() {
		var turn func(...sdk.ContentBlockParamUnion) sdk.MessageParam
		switch message.Role() {
		case roles.User:
			turn = sdk.NewUserMessage
		case roles.Assistant:
			turn = sdk.NewAssistantMessage
		default:
			return sdk.MessageNewParams{}, errors.New(
				"the exchange holds a turn taken by nobody, which this API has no part for")
		}

		content := blocks(message)
		if len(content) == 0 {
			continue
		}
		turns = append(turns, turn(content...))
	}

	parameters := sdk.MessageNewParams{
		Model:     sdk.Model(p.model),
		MaxTokens: p.tokens,
		Messages:  turns,
		Tools:     catalogue(registry),
	}

	if len(thread.Instructions()) > 0 {
		parameters.System = []sdk.TextBlockParam{
			{Text: thread.Instructions()},
		}
	}

	return parameters, nil
}

// provider is the adapter itself. It is unexported because callers hold the
// port, not this type; [New] is the only way to get one.
type provider struct {
	client sdk.Client
	model  string
	tokens int64
}

// reply turns the response's content blocks into one assistant message
// carrying all of them, in the order they arrived. Text arrives as itself; a
// tool call arrives whole — id, name and the arguments as the model sent
// them, which is what pairs the answer with the call later; and so does the
// reasoning that led to either. A block this mapping has no shape for is
// skipped rather than guessed at, an empty text block is skipped because it
// says nothing and cannot be sent back, and a response with nothing left after
// that is no message at all — which [provider.Invoke] reports as an error,
// since it is the one holding an error return.
//
// The model's own reasoning is among what is skipped. It arrives — no thinking
// parameter is sent, which on the models this adapter reaches is what leaves
// the model reasoning — and the core has no shape to put it in, so it is
// dropped here like everything else the port does not carry. A response that
// was nothing but reasoning therefore leaves no content at all, which is the
// empty response above reached by another road.
func reply(response *sdk.Message) []messages.Type {
	content := []messages.Content{}
	for _, block := range response.Content {
		// A block with nothing in it is not something the model said,
		// and it is not something that can be sent back: the API
		// refuses a text block that is empty, so keeping one would put
		// a turn in the record that makes every request after it fail.
		// Dropping it here is why the check in [blocks] is only ever
		// about turns written before this one existed.
		if text, ok := block.AsAny().(sdk.TextBlock); ok && text.Text != "" {
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
		return ErrCutOff
	case sdk.StopReasonRefusal:
		return ErrDeclined
	case sdk.StopReasonModelContextWindowExceeded:
		return ErrContext
	default:
		return nil
	}
}

// failed reports what the API said when it would not answer, or hands back
// what it was given where the failure was not the API's to report. It is the
// request side of what [refused] does for the response: a status code is this
// vendor's way of accounting for itself, the core has no vocabulary for one
// and should not grow one, so it is read here and spent here.
//
// A 404 is reported as the model rather than as a thing not found: the address
// this adapter posts to is fixed, so the only part of it a caller can get
// wrong is which model they asked for. A 403 is two different things — a key
// without access and an account that cannot be billed — and only the API's own
// name for the error tells them apart. Every 5xx is one thing, including the
// 529 this API answers when it is overloaded: it was not the request's fault
// and it is worth trying again.
//
// Anything that is not the API refusing goes back untouched, so that a
// cancelled request still reaches the caller as [context.Canceled] and an
// interrupt ends the exchange rather than being reported as something the API
// did.
func failed(err error) error {
	var refusal *sdk.Error
	if !errors.As(err, &refusal) {
		return err
	}

	switch refusal.StatusCode {
	case http.StatusBadRequest:
		return ErrRequest
	case http.StatusUnauthorized:
		return ErrCredentials
	case http.StatusForbidden:
		if refusal.Type() == sdk.ErrorTypeBillingError {
			return ErrBilling
		}
		return ErrPermission
	case http.StatusNotFound:
		return ErrModel
	case http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case http.StatusTooManyRequests:
		return ErrRateLimited
	}

	if refusal.StatusCode >= 500 {
		return ErrUnavailable
	}

	return ErrRefused
}

// Invoke implements [inference.Provider]. One round trip: the thread and the
// tools on offer go out as request parameters, and the reply comes back whole
// — including a request to run one of those tools, which is content like any
// other and the caller's to act on. A request the API refuses comes back as an
// error rather than as an empty reply, and so does a response that is no reply:
// one the model stopped short of finishing, and one carrying nothing this
// adapter has a shape for. A thread this adapter cannot build a request from
// is refused before the round trip is paid for. Ending an exchange silently and ending it well look
// the same to a caller, which is reason enough not to let the first happen.
func (p *provider) Invoke(ctx Context, thread threads.Type, registry tools.Registry) ([]messages.Type, error) {
	parameters, err := p.parameters(thread, registry)
	if err != nil {
		return nil, err
	}

	response, err := p.client.Messages.New(ctx, parameters)
	if err != nil {
		return nil, failed(err)
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
// The model and the ceiling on one reply are arguments because they are the
// caller's to choose, and an adapter built with the wrong one is not degraded
// but pointed at a different model. A caller with no opinion says so by
// passing an empty model or a ceiling of zero, and gets [Model] and [Tokens];
// that is a fallback rather than validation, so that a value nobody chose can
// never go out as a request asking for no tokens at all.
//
// Credentials come from the environment unless an [Option] says otherwise.
func New(model string, tokens int64, options ...Option) inference.Provider {
	if model == "" {
		model = Model
	}
	if tokens <= 0 {
		tokens = Tokens
	}

	return &provider{
		client: sdk.NewClient(options...),
		model:  model,
		tokens: tokens,
	}
}
