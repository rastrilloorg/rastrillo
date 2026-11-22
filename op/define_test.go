package op

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	json "github.com/go-json-experiment/json"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

type noteInput struct {
	Title string `json:"title"`
}

type note struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// counted is the fixture's run: it makes a note from its input and
// counts its calls, so a test can tell whether a refusal came first.
type counted struct{ calls int }

func (counter *counted) create(
	_ context.Context, _ *rastrillo.Ctx, input noteInput,
) (note, error) {
	counter.calls++
	return note{ID: 1, Title: input.Title}, nil
}

func defineCreate(counter *counted) *Named[noteInput, note] {
	return Define("notes.create", Spec{
		Description: "Create a note.", Access: rastrillo.Write,
	}, counter.create)
}

// person is a signed-in browser session, which DefaultPolicy allows on
// an unscoped operation.
func person() *rastrillo.Ctx {
	return &rastrillo.Ctx{
		Actor: rastrillo.Actor{Subject: "ana", Access: rastrillo.Write},
	}
}

func propertyNames(t *testing.T, schema []byte) []string {
	t.Helper()
	var decoded struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema %s: %v", schema, err)
	}
	var names []string
	for name := range decoded.Properties {
		names = append(names, name)
	}
	return names
}

func TestDefineBuildsTheDescriptor(t *testing.T) {
	descriptor := defineCreate(&counted{}).Descriptor
	if descriptor.ID != "notes.create" {
		t.Errorf("ID = %q, want notes.create", descriptor.ID)
	}
	if descriptor.Resource != "notes" {
		t.Errorf("Resource = %q, want notes", descriptor.Resource)
	}
	if descriptor.Access != rastrillo.Write {
		t.Errorf("Access = %v, want write", descriptor.Access)
	}
	if !descriptor.Exposed {
		t.Error("Exposed = false, want true")
	}
	if descriptor.Description != "Create a note." {
		t.Errorf("Description = %q, want Create a note.",
			descriptor.Description)
	}
	if descriptor.Destructive || descriptor.Idempotent {
		t.Errorf("Destructive, Idempotent = %v, %v, want false, false",
			descriptor.Destructive, descriptor.Idempotent)
	}
	inputs := propertyNames(t, descriptor.Input)
	if !reflect.DeepEqual(inputs, []string{"title"}) {
		t.Errorf("Input properties = %v, want [title]", inputs)
	}
	results := propertyNames(t, descriptor.Result)
	slices.Sort(results)
	if strings.Join(results, " ") != "id title" {
		t.Errorf("Result properties = %v, want [id title]", results)
	}
	if descriptor.Invoke == nil {
		t.Error("Invoke is nil")
	}
}

func TestDefineCarriesTheSpecsTextAndFlags(t *testing.T) {
	read := Define("notes.search", Spec{
		Description: "Search your notes.", Confirm: "Search?",
		Access: rastrillo.Read, Idempotent: true, Scoped: true,
		InQuery: true,
	}, func(context.Context, *rastrillo.Ctx, noteInput) (None, error) {
		return None{}, nil
	}).Descriptor
	if read.Confirm != "Search?" || !read.Idempotent || !read.Scoped ||
		!read.InQuery || read.Access != rastrillo.Read {
		t.Errorf("descriptor = %+v, want the spec's Confirm, Idempotent, "+
			"Scoped, InQuery and Access", read)
	}
	purge := Define("notes.purge", Spec{
		Description: "Purge the bin.", Access: rastrillo.Write,
		Destructive: true,
	}, func(context.Context, *rastrillo.Ctx, None) (None, error) {
		return None{}, nil
	}).Descriptor
	if !purge.Destructive {
		t.Error("Destructive = false, want the spec's true")
	}
}

func TestCallChecksBeforeItRuns(t *testing.T) {
	counter := &counted{}
	create := defineCreate(counter)
	rc := person()
	rc.Authorize = func(
		context.Context, rastrillo.Actor, rastrillo.Operation,
	) error {
		return Forbidden{Reason: "not today"}
	}
	_, err := create.Call(context.Background(), rc,
		noteInput{Title: "a"})
	var forbidden Forbidden
	if !errors.As(err, &forbidden) || forbidden.Reason != "not today" {
		t.Errorf("Call = %v, want the policy's Forbidden", err)
	}
	if counter.calls != 0 {
		t.Errorf("run ran %d times, want 0", counter.calls)
	}
	created, err := create.Call(context.Background(), person(),
		noteInput{Title: "a"})
	if err != nil || created != (note{ID: 1, Title: "a"}) {
		t.Errorf("Call = %+v, %v, want note 1 titled a", created, err)
	}
}

func TestInvokeDecodesWithTheAPIRules(t *testing.T) {
	counter := &counted{}
	invoke := defineCreate(counter).Descriptor.Invoke
	if invoke == nil {
		t.Fatal("Invoke is nil")
	}
	ctx := context.Background()
	_, err := invoke(ctx, person(), []byte(`{"title":"a","colour":1}`))
	var invalid Invalid
	if !errors.As(err, &invalid) || !invalid.AsSent ||
		invalid.Fields["colour"] != msgUnknown {
		t.Errorf("Invoke = %v, want Invalid as sent on colour", err)
	}
	if counter.calls != 0 {
		t.Errorf("run ran %d times, want 0", counter.calls)
	}
	value, err := invoke(ctx, person(), []byte(`{"title":"a"}`))
	if err != nil || value != (note{ID: 1, Title: "a"}) {
		t.Errorf("Invoke = %#v, %v, want note 1 titled a", value, err)
	}
}

func TestAnOperationWithNoResultInvokesToNil(t *testing.T) {
	remove := Define("notes.delete", Spec{
		Description: "Delete a note.", Access: rastrillo.Write,
		Destructive: true,
	}, func(context.Context, *rastrillo.Ctx, noteInput) (None, error) {
		return None{}, nil
	})
	if string(remove.Descriptor.Result) != `{"type":"null"}` {
		t.Errorf("Result = %s, want {\"type\":\"null\"}",
			remove.Descriptor.Result)
	}
	if remove.Descriptor.Invoke == nil {
		t.Fatal("Invoke is nil")
	}
	value, err := remove.Descriptor.Invoke(context.Background(), person(),
		[]byte(`{"title":"a"}`))
	if value != nil || err != nil {
		t.Errorf("Invoke = %#v, %v, want nil, nil", value, err)
	}
}

func TestSpecHumanOnlyReachesTheOperation(t *testing.T) {
	counter := &counted{}
	const why = "Only you can close your account."
	closeAccount := Define("account.close", Spec{
		Description: "Close the account.", Access: rastrillo.Write,
		HumanOnly: why,
	}, counter.create)
	if closeAccount.Descriptor.HumanOnly != why {
		t.Errorf("HumanOnly = %q, want %q",
			closeAccount.Descriptor.HumanOnly, why)
	}
	token := &rastrillo.Ctx{Actor: rastrillo.Actor{
		Subject: "ana", Credential: "tok", Access: rastrillo.Write,
	}}
	_, err := closeAccount.Call(context.Background(), token, noteInput{})
	if !errors.As(err, new(HumanOnly)) {
		t.Errorf("Call with a token = %v, want HumanOnly", err)
	}
	if counter.calls != 0 {
		t.Errorf("run ran %d times, want 0", counter.calls)
	}
}

type tagged struct {
	Tags []string `json:"tags"`
}

type untitled struct {
	Title string
}

// ownWriter writes itself, so its schema can't promise an object.
type ownWriter struct{}

func (ownWriter) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }

func TestDefineRefusesWhatWouldFailAtADoor(t *testing.T) {
	write := Spec{Description: "Create a note.", Access: rastrillo.Write}
	read := Spec{Description: "List notes.", Access: rastrillo.Read}
	destructiveRead, queryWrite, query := read, write, read
	destructiveRead.Destructive = true
	queryWrite.InQuery = true
	query.InQuery = true
	run := (&counted{}).create
	long := "notes." + strings.Repeat("a", 59)
	prefixed := func(id, message string) string {
		return "op: " + id + ": " + message
	}
	cases := []struct {
		name   string
		define func()
		want   string
	}{
		{"a capital letter", func() { Define("Notes.create", write, run) },
			"op: " + fmt.Sprintf(msgBadID, "Notes.create")},
		{"no verb", func() { Define("notes", write, run) },
			"op: " + fmt.Sprintf(msgBadID, "notes")},
		{"two dots", func() { Define("notes.create.now", write, run) },
			"op: " + fmt.Sprintf(msgBadID, "notes.create.now")},
		{"a long tool name", func() { Define(long, write, run) },
			prefixed(long, fmt.Sprintf(msgLongToolName,
				strings.Replace(long, ".", "_", 1), 65))},
		{"no description", func() {
			Define("notes.create", Spec{Access: rastrillo.Write}, run)
		}, prefixed("notes.create", msgNoDescription)},
		{"no access", func() {
			Define("notes.create", Spec{Description: "Create."}, run)
		}, prefixed("notes.create", msgBadAccess)},
		{"a nil run", func() {
			Define[noteInput, note]("notes.create", write, nil)
		}, prefixed("notes.create", msgNilRun)},
		{"a string input", func() {
			Define("notes.create", write, func(
				context.Context, *rastrillo.Ctx, string) (note, error) {
				return note{}, nil
			})
		}, prefixed("notes.create",
			fmt.Sprintf(msgInputNotStruct, "string"))},
		{"an int result", func() {
			Define("notes.create", write, func(
				context.Context, *rastrillo.Ctx, noteInput) (int, error) {
				return 0, nil
			})
		}, prefixed("notes.create",
			fmt.Sprintf(msgResultNotStruct, "int"))},
		{"a time input", func() {
			Define("notes.create", write, func(
				context.Context, *rastrillo.Ctx, time.Time) (note, error) {
				return note{}, nil
			})
		}, prefixed("notes.create",
			fmt.Sprintf(msgInputNotObject, "time.Time"))},
		{"a time result", func() {
			Define("notes.create", write, func(
				context.Context, *rastrillo.Ctx, noteInput) (time.Time,
				error) {
				return time.Time{}, nil
			})
		}, prefixed("notes.create",
			fmt.Sprintf(msgResultNotObject, "time.Time"))},
		{"a result that writes itself", func() {
			Define("notes.create", write, func(
				context.Context, *rastrillo.Ctx, noteInput) (ownWriter,
				error) {
				return ownWriter{}, nil
			})
		}, prefixed("notes.create",
			fmt.Sprintf(msgResultNotObject, "op.ownWriter"))},
		{"an untagged input field", func() {
			Define("notes.create", write, func(
				context.Context, *rastrillo.Ctx, untitled) (note, error) {
				return note{}, nil
			})
		}, prefixed("notes.create",
			fmt.Sprintf(msgUntagged, "untitled", "Title", "title"))},
		{"a destructive read", func() {
			Define("notes.create", destructiveRead, run)
		}, "op: notes.create is a read and cannot be Destructive"},
		{"a delete that isn't destructive", func() {
			Define("notes.delete", write, run)
		}, `op: notes.delete starts with "delete", so it must be Destructive`},
		{"InQuery on a write", func() {
			Define("notes.create", queryWrite, run)
		}, prefixed("notes.create", msgQueryWrite)},
		{"InQuery with a list", func() {
			Define("notes.list", query, func(
				context.Context, *rastrillo.Ctx, tagged) (note, error) {
				return note{}, nil
			})
		}, prefixed("notes.list", fmt.Sprintf(msgQueryNotFlat, "tags"))},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			got := panicOf(each.define)
			if got == "" {
				t.Fatalf("Define did not panic, want %q", each.want)
			}
			if got != each.want {
				t.Errorf("panic = %q\nwant    %q", got, each.want)
			}
		})
	}
}

func TestInQueryTakesEveryFlatField(t *testing.T) {
	type filters struct {
		Search Opt[string]  `json:"search"`
		Limit  Opt[int]     `json:"limit"`
		Pinned Opt[bool]    `json:"pinned"`
		Above  Opt[float64] `json:"above"`
	}
	got := panicOf(func() {
		Define("notes.list", Spec{
			Description: "List notes.", Access: rastrillo.Read,
			InQuery: true,
		}, func(context.Context, *rastrillo.Ctx, filters) (note, error) {
			return note{}, nil
		})
	})
	if got != "" {
		t.Errorf("Define panicked: %s", got)
	}
}

// panicOf is what define panics with, or "" when it returns.
func panicOf(define func()) (message string) {
	defer func() {
		if value := recover(); value != nil {
			message = fmt.Sprint(value)
		}
	}()
	define()
	return ""
}

func TestAnOperationThatTakesNoneTakesAnEmptyObject(t *testing.T) {
	purge := Define("notes.purge", Spec{
		Description: "Purge the bin.", Access: rastrillo.Write,
		Destructive: true,
	}, func(context.Context, *rastrillo.Ctx, None) (None, error) {
		return None{}, nil
	}).Descriptor
	want := `{"type":"object","properties":{},"additionalProperties":false}`
	if string(purge.Input) != want {
		t.Errorf("Input = %s, want %s", purge.Input, want)
	}
	value, err := purge.Invoke(context.Background(), person(), []byte(`{}`))
	if value != nil || err != nil {
		t.Errorf("Invoke({}) = %#v, %v, want nil, nil", value, err)
	}
}

// probed counts its decodes, so a test can tell whether Invoke read
// the input before it checked the caller.
type probed struct{}

var probeDecodes int

func (*probed) UnmarshalJSON([]byte) error {
	probeDecodes++
	return nil
}

type probedInput struct {
	Probe probed `json:"probe"`
}

func TestInvokeChecksTheCallerBeforeItReadsTheInput(t *testing.T) {
	counter := &counted{}
	closeAccount := Define("account.close", Spec{
		Description: "Close the account.", Access: rastrillo.Write,
		HumanOnly: "Only you can close your account.",
	}, func(
		ctx context.Context, rc *rastrillo.Ctx, _ probedInput,
	) (note, error) {
		return counter.create(ctx, rc, noteInput{})
	})
	agent := &rastrillo.Ctx{Actor: rastrillo.Actor{Subject: "ana",
		Agent: "Claude", Credential: "tok", Access: rastrillo.Write}}
	probeDecodes = 0
	for _, sent := range []string{`{"probe":`, `{"probe":1}`} {
		_, err := closeAccount.Descriptor.Invoke(context.Background(),
			agent, []byte(sent))
		if !errors.As(err, new(HumanOnly)) {
			t.Errorf("Invoke(%s) as an agent = %v, want HumanOnly", sent, err)
		}
	}
	if probeDecodes != 0 || counter.calls != 0 {
		t.Errorf("the decoder ran %d times and run %d, want 0 and 0",
			probeDecodes, counter.calls)
	}
}
