package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/core/usage"
	"github.com/ksahli/compadre/internal/stores/sqlite"
)

// said renders one content block as a line a case can state, naming the shape
// it turned out to be. A round trip is only worth anything if it is the same
// shape that comes back, so the rendering says which one it was.
func said(content messages.Content) string {
	if text, ok := content.Text(); ok {
		return "text:" + text
	}
	if request, ok := content.Use(); ok {
		return "use:" + request.ID() + ":" + request.Name() + ":" + string(request.Arguments())
	}
	if result, ok := content.Result(); ok {
		failed := "ok"
		if result.Failed() {
			failed = "failed"
		}
		return "result:" + result.ID() + ":" + failed + ":" + result.Content()
	}
	return "unknown"
}

// record is every message of a thread, one entry each.
func record(thread threads.Type) []string {
	got := []string{}
	for _, message := range thread.Messages() {
		blocks := []string{}
		for _, content := range message.Content() {
			blocks = append(blocks, said(content))
		}
		got = append(got, message.Role().String()+"("+strings.Join(blocks, "|")+")")
	}
	return got
}

// open is a store in a directory of the test's own, so that every case starts
// from an empty database and none can see another's.
func open(t *testing.T) *sqlite.Store {
	t.Helper()

	store, _ := opened(t)
	return store
}

// opened is [open] with the path handed back, for the one test that has to go
// behind the store and write a row it would never write itself.
func opened(t *testing.T) (*sqlite.Store, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "exchanges.db")
	store, err := sqlite.New(path)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	t.Cleanup(func() { store.Close() })

	return store, path
}

func TestSaveAndLoad(t *testing.T) {
	cases := []struct {
		name   string
		thread threads.Type
		want   []string
	}{
		{
			name:   "a turn that was said",
			thread: threads.New("be brief", messages.New(roles.User, messages.Text("hello"))),
			want:   []string{"User(text:hello)"},
		},
		{
			// The three shapes in one exchange, which is what an
			// exchange with a tool in it actually looks like.
			name: "a call, its answer, and what was said around them",
			thread: threads.New("be brief",
				messages.New(roles.User, messages.Text("what is the weather?")),
				messages.New(roles.Assistant,
					messages.Text("let me look"),
					messages.Use(use.New("toolu_1", "weather", use.Arguments(`{"city":"Paris"}`))),
					messages.Use(use.New("toolu_2", "weather", use.Arguments(`{"city":"Nice"}`))),
				),
				messages.New(roles.User,
					messages.Result(tools.Success("toolu_1", "17C")),
					messages.Result(tools.Failure("toolu_2", errors.New("no such city"))),
				),
				messages.New(roles.Assistant, messages.Text("17C in Paris")),
			),
			want: []string{
				"User(text:what is the weather?)",
				`Assistant(text:let me look|use:toolu_1:weather:{"city":"Paris"}|use:toolu_2:weather:{"city":"Nice"})`,
				"User(result:toolu_1:ok:17C|result:toolu_2:failed:no such city)",
				"Assistant(text:17C in Paris)",
			},
		},
		{
			// A tool that takes nothing is called with nothing,
			// and comes back that way.
			name: "a call with no arguments",
			thread: threads.New("",
				messages.New(roles.Assistant, messages.Use(use.New("toolu_1", "pwd", nil)))),
			want: []string{"Assistant(use:toolu_1:pwd:)"},
		},
		{
			// Arguments that do not parse are stored as they
			// arrived, here as everywhere else.
			name: "arguments that are not valid JSON",
			thread: threads.New("",
				messages.New(roles.Assistant,
					messages.Use(use.New("toolu_1", "read", use.Arguments(`{"path":`))))),
			want: []string{`Assistant(use:toolu_1:read:{"path":)`},
		},
		{
			// A turn with nothing in it is still a turn, and has
			// to come back as one rather than be dropped.
			name:   "a turn that says nothing",
			thread: threads.New("", messages.New(roles.Assistant)),
			want:   []string{"Assistant()"},
		},
		{
			// An exchange with no turns yet is legal: it is what a
			// caller has before anything has been said.
			name:   "an exchange with no turns",
			thread: threads.New("be brief"),
			want:   []string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, ctx := open(t), context.Background()

			saved, err := store.Save(ctx, exchanges.Open(c.thread))
			if err != nil {
				t.Fatalf("Save() error = %v, want nil", err)
			}
			if saved.ID() == "" {
				t.Fatal("Save() filed the exchange under no id, want one")
			}

			loaded, err := store.Load(ctx, saved.ID())
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}

			if got := record(loaded.Thread()); !slices.Equal(got, c.want) {
				t.Errorf("thread = %v, want %v", got, c.want)
			}
			if got, want := loaded.Thread().Instructions(), c.thread.Instructions(); got != want {
				t.Errorf("Instructions() = %q, want %q", got, want)
			}
			if got := loaded.ID(); got != saved.ID() {
				t.Errorf("Type.ID() = %q, want %q", got, saved.ID())
			}
		})
	}
}

// TestSaveAppendsRatherThanRewrites pins what makes a write after every turn
// cheap. An exchange only ever grows, so saving one that has grown writes the
// turns that are new and leaves the rest where they were — it does not file a
// second exchange and does not write the early turns twice.
func TestSaveAppendsRatherThanRewrites(t *testing.T) {
	store, ctx := open(t), context.Background()

	opened := threads.New("be brief", messages.New(roles.User, messages.Text("hello")))

	saved, err := store.Save(ctx, exchanges.Open(opened))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	grown := saved.With(saved.Thread().Append(
		messages.New(roles.Assistant, messages.Text("hola"))))

	again, err := store.Save(ctx, grown)
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if got := again.ID(); got != saved.ID() {
		t.Errorf("Type.ID() = %q, want %q — the second save opened a second exchange", got, saved.ID())
	}

	loaded, err := store.Load(ctx, saved.ID())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := []string{"User(text:hello)", "Assistant(text:hola)"}
	if got := record(loaded.Thread()); !slices.Equal(got, want) {
		t.Errorf("thread = %v, want %v", got, want)
	}
}

// TestSaveOfAnExchangeThatWasLoaded pins the round trip a resumed run is: an
// exchange read back out of the store, grown by a turn, and written down
// again. It is not the same as appending to an exchange still in hand, which
// TestSaveAppendsRatherThanRewrites covers — here the count of what is already
// filed has to be arrived at from the id alone, and getting it wrong would
// write the whole conversation down a second time under the same thread.
func TestSaveOfAnExchangeThatWasLoaded(t *testing.T) {
	store, ctx := open(t), context.Background()

	saved, err := store.Save(ctx, exchanges.Open(threads.New("be brief",
		messages.New(roles.User, messages.Text("why is the sky blue?")),
		messages.New(roles.Assistant, messages.Text("rayleigh scattering")),
	)))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	loaded, err := store.Load(ctx, saved.ID())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	resumed, err := store.Save(ctx, loaded.With(loaded.Thread().Append(
		messages.New(roles.User, messages.Text("and at sunset?")))))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if got := resumed.ID(); got != saved.ID() {
		t.Errorf("Type.ID() = %q, want %q — resuming opened a second exchange", got, saved.ID())
	}

	again, err := store.Load(ctx, saved.ID())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := []string{
		"User(text:why is the sky blue?)",
		"Assistant(text:rayleigh scattering)",
		"User(text:and at sunset?)",
	}
	if got := record(again.Thread()); !slices.Equal(got, want) {
		t.Errorf("thread = %v, want %v", got, want)
	}
	// The instructions are not a turn, so nothing appends them: they have
	// to have survived the trip out and back rather than been written down
	// again or lost.
	if got, want := again.Thread().Instructions(), "be brief"; got != want {
		t.Errorf("Thread.Instructions() = %q, want %q", got, want)
	}
}

// TestSaveOfAnExchangeThatShrank pins the refusal. A thread shorter than what
// is filed under it is not one that grew, and the store has no business
// guessing which of the two is the record.
func TestSaveOfAnExchangeThatShrank(t *testing.T) {
	store, ctx := open(t), context.Background()

	saved, err := store.Save(ctx, exchanges.Open(threads.New("",
		messages.New(roles.User, messages.Text("hello")),
		messages.New(roles.Assistant, messages.Text("hola")))))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	shrunk := saved.With(threads.New("", messages.New(roles.User, messages.Text("hello"))))

	if _, err := store.Save(ctx, shrunk); err == nil {
		t.Fatal("Save() error = nil, want an error")
	}
}

// TestLoadOfAnExchangeThatIsNotThere pins that a missing exchange is an error
// rather than an empty one. The two are different answers, and a caller told
// they are the same would go on to continue a conversation that never
// happened.
func TestLoadOfAnExchangeThatIsNotThere(t *testing.T) {
	store, ctx := open(t), context.Background()

	for _, id := range []string{"404", "", "not-a-row"} {
		t.Run("an id of "+`"`+id+`"`, func(t *testing.T) {
			if _, err := store.Load(ctx, id); err == nil {
				t.Fatal("Load() error = nil, want an error")
			}
		})
	}
}

// TestNewOnAnExistingStore pins that applying the schema is idempotent:
// opening a store that has been used before is the same call as opening a
// fresh one, and what was in it is still there.
func TestNewOnAnExistingStore(t *testing.T) {
	path, ctx := filepath.Join(t.TempDir(), "exchanges.db"), context.Background()

	first, err := sqlite.New(path)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	saved, err := first.Save(ctx, exchanges.Open(threads.New("be brief",
		messages.New(roles.User, messages.Text("hello")))))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}

	second, err := sqlite.New(path)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	defer second.Close()

	loaded, err := second.Load(ctx, saved.ID())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if got, want := record(loaded.Thread()), []string{"User(text:hello)"}; !slices.Equal(got, want) {
		t.Errorf("thread = %v, want %v", got, want)
	}
}

// TestLoadOfATurnTakenByNobody pins what a record written by something else
// gets: the role column is text, so a row can hold a word the core has no role
// for, and reading it as a turn would carry that word into a thread nothing
// downstream can place. It is refused at the read instead, where the bad value
// came in.
func TestLoadOfATurnTakenByNobody(t *testing.T) {
	store, path := opened(t)
	ctx := context.Background()

	saved, err := store.Save(ctx, exchanges.Open(threads.New("",
		messages.New(roles.User, messages.Text("hello")))))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE messages SET role = 'Stranger'`); err != nil {
		t.Fatalf("UPDATE error = %v, want nil", err)
	}

	if _, err := store.Load(ctx, saved.ID()); !errors.Is(err, roles.ErrUnknown) {
		t.Errorf("Load() error = %v, want %v", err, roles.ErrUnknown)
	}
}

// TestSaveAndLoadKeepsWhatTheTurnCost is the round trip for the counts. The
// model's turns carry one and the caller's do not, and both facts have to come
// back as they went in: a turn nobody counted must not read back as one
// counted at nothing.
func TestSaveAndLoadKeepsWhatTheTurnCost(t *testing.T) {
	store := open(t)

	thread := threads.New("",
		messages.New(roles.User, messages.Text("why is the sky blue?")),
		messages.New(roles.Assistant, messages.Text("rayleigh scattering")).With(usage.New(1204, 318)),
		messages.New(roles.User, messages.Text("and at sunset?")),
		messages.New(roles.Assistant, messages.Text("the same, longer through it")).With(usage.New(0, 0)),
	)

	saved, err := store.Save(context.Background(), exchanges.Open(thread))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	loaded, err := store.Load(context.Background(), saved.ID())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := []usage.Type{{}, usage.New(1204, 318), {}, usage.New(0, 0)}
	got := loaded.Thread().Messages()
	if len(got) != len(want) {
		t.Fatalf("loaded %d turns, want %d", len(got), len(want))
	}
	for at, count := range want {
		if got[at].Usage() != count {
			t.Errorf("turn %d cost %v, want %v", at, got[at].Usage(), count)
		}
	}
}

// The counts belong to the turn rather than to a block, so a turn made of
// several blocks reads back carrying one count and not the last row's.
func TestLoadCountsATurnOnce(t *testing.T) {
	store := open(t)

	turn := messages.New(roles.Assistant,
		messages.Text("looking"),
		messages.Use(use.New("toolu_1", "read_file", use.Arguments(`{"path":"go.mod"}`))),
	).With(usage.New(9, 4))

	saved, err := store.Save(context.Background(), exchanges.Open(threads.New("", turn)))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	loaded, err := store.Load(context.Background(), saved.ID())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	got := loaded.Thread().Messages()
	if len(got) != 1 {
		t.Fatalf("loaded %d turns, want 1", len(got))
	}
	if got[0].Usage() != usage.New(9, 4) {
		t.Errorf("the turn cost %v, want 9 in and 4 out", got[0].Usage())
	}
	if len(got[0].Content()) != 2 {
		t.Errorf("the turn said %d blocks, want 2", len(got[0].Content()))
	}
}
