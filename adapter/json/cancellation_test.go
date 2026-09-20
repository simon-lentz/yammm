package json

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const cancelSchema = `schema "c"

type Alpha {
	id String primary
}

type Beta {
	id String primary
}
`

// cancelSnapshot builds a snapshot holding two type groups, so a per-type-group
// check has a group boundary to stop at.
func cancelSnapshot(t *testing.T) *graph.Snapshot {
	t.Helper()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, cancelSchema, "c.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	g := graph.New(s)
	for _, tn := range []string{"Alpha", "Beta"} {
		ty, ok := s.Type(tn)
		if !ok {
			t.Fatalf("no %s type", tn)
		}
		inst := instance.NewValidInstance(tn, ty.ID(),
			immutable.WrapKey([]any{"k1"}),
			immutable.WrapProperties(map[string]any{"id": "k1"}), nil, nil, nil)
		if r := g.Add(ctx, inst); r.Err() != nil {
			t.Fatalf("add %s: %v", tn, r.Err())
		}
	}
	return g.Snapshot()
}

// A cancelled context stops the parse and says so, at Fatal: a caller testing
// HasFatal to mean the run did not finish must read true for an abandoned run.
func TestParseObject_CancelledContextIsFatal(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	doc := []byte(`{"Alpha":[{"id":"k1"}],"Beta":[{"id":"k2"}]}`)
	_, res := New().ParseObject(ctx, location.NewSourceID("c.json"), doc)

	if !res.HasFatal() {
		t.Fatalf("a cancelled parse reported no Fatal issue: %s", res.String())
	}
	var found bool
	for iss := range res.Issues() {
		if iss.Code() == diag.E_CONTEXT_CANCELLED {
			found = true
			if iss.Severity() != diag.Fatal {
				t.Errorf("E_CONTEXT_CANCELLED at %v, want Fatal", iss.Severity())
			}
		}
	}
	if !found {
		t.Errorf("no E_CONTEXT_CANCELLED issue: %s", res.String())
	}
}

// The writer stops on a cancelled context and returns the cause, as its CSV
// sibling does; MarshalObject returns an error rather than a diag.Result.
func TestMarshalObject_CancelledContextReturnsTheCause(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New().MarshalObject(ctx, cancelSnapshot(t))
	if err == nil {
		t.Fatal("a cancelled marshal returned no error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
}

// A cancelled write produces no document: the cancellation is observed while
// the document is built, so WriteObject returns before it writes a byte.
func TestWriteObject_CancelledContextWritesNothing(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	n, err := New().WriteObject(ctx, &buf, cancelSnapshot(t))
	if err == nil {
		t.Fatal("a cancelled write returned no error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("a cancelled write produced %d bytes (%q), want none", buf.Len(), buf.String())
	}
}

// A live context is untouched by the checks: the document is whole.
func TestMarshalObject_LiveContextIsUnaffected(t *testing.T) {
	t.Parallel()
	doc, err := New().MarshalObject(context.Background(), cancelSnapshot(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"Alpha"`, `"Beta"`} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("wrote %s, want it to contain %s", doc, want)
		}
	}
}

// cancelAfterPolls reports itself cancelled from its n-th Err poll on, so a
// parse can be stopped at a chosen key boundary rather than before it starts.
// Nothing here selects on Done. It mirrors the stub of the same name in
// snapshot's tests; each package keeps its own, since neither is exported.
type cancelAfterPolls struct {
	mu    sync.Mutex
	polls int
	after int
}

func (c *cancelAfterPolls) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterPolls) Done() <-chan struct{}       { return nil }
func (c *cancelAfterPolls) Value(any) any               { return nil }

func (c *cancelAfterPolls) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.polls++
	if c.polls >= c.after {
		return context.Canceled
	}
	return nil
}

// A parse cancelled PARTWAY keeps the keys it already read, as the CSV parser
// keeps its records, so a caller can tell an abandoned run from an empty one.
// Cancelling before the parse cannot show this: it keeps nothing, which is
// what an empty document returns.
func TestParseObject_CancelledParseKeepsWhatItRead(t *testing.T) {
	t.Parallel()

	const keys = 50
	var sb strings.Builder
	sb.WriteString(`{`)
	for i := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"T%d":[{"id":"k"}]`, i)
	}
	sb.WriteString(`}`)

	// Live for the first three polls, so the parse reads keys before stopping.
	ctx := &cancelAfterPolls{after: 4}
	got, res := New().ParseObject(ctx, location.NewSourceID("c.json"), []byte(sb.String()))

	if !res.HasFatal() {
		t.Fatalf("a cancelled parse reported no Fatal issue: %s", res.String())
	}
	if len(got) == 0 {
		t.Fatal("a cancelled parse kept nothing, want the keys it read")
	}
	if len(got) >= keys {
		t.Errorf("a cancelled parse kept %d of %d keys, want it to stop short", len(got), keys)
	}
}

// An empty snapshot has no type group, so buildOutput's loop never polls the
// context and the marshal succeeds. The check before the write is the only
// one that can stop it, and without that check a cancelled run writes "{}".
func TestWriteObject_CancelledContextWritesNothingForAnEmptySnapshot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, res := schema.LoadString(context.Background(), cancelSchema, "c.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	empty := graph.New(s).Snapshot()
	if len(empty.Types()) != 0 {
		t.Fatalf("fixture is not empty: %d type groups", len(empty.Types()))
	}

	var buf bytes.Buffer
	n, err := New().WriteObject(ctx, &buf, empty)
	if err == nil {
		t.Fatal("a cancelled write of an empty snapshot returned no error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("a cancelled write produced %d bytes (%q), want none", buf.Len(), buf.String())
	}
}
