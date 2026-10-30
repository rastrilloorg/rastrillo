package action_test

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/op"
	"github.com/go-chi/chi/v5"
)

// showingAgain records what the action asked a form to show, so a test
// can see the status and the errors without drawing a template.
type showingAgain struct {
	calls  int
	status int
	errors form.Errors
}

func (again *showingAgain) ShowAgain(
	_ *action.Visit, status int, errors form.Errors,
) {
	again.calls++
	again.status = status
	again.errors = errors
}

// serve runs one request through a chi router with POST /things/{id},
// the way a generated route table does, and hands the Visit it built to
// act. The recorder comes back for the test to read the answer from.
func serve(
	t testing.TB, body string, act func(visit *action.Visit),
) *httptest.ResponseRecorder {
	t.Helper()
	return serveAs(t, "application/x-www-form-urlencoded", body, act)
}

// serveAs is serve with the body's Content-Type chosen by the test.
func serveAs(
	t testing.TB, contentType, body string, act func(visit *action.Visit),
) *httptest.ResponseRecorder {
	t.Helper()
	return serveAt(t, http.MethodPost, "/things/1", contentType, body, act)
}

// serveAt is serveAs with the method and path chosen too. The route is
// /things/{id} for that method. The assertions
// of most form tests sit inside act, so a request that never reaches it
// would leave them unrun and the test green; serveAt fails the test
// then, and when act runs more than once.
func serveAt(
	t testing.TB, method, path, contentType, body string,
	act func(visit *action.Visit),
) *httptest.ResponseRecorder {
	t.Helper()
	calls := 0
	router := chi.NewRouter()
	router.Method(method, "/things/{id}", http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		calls++
		act(action.NewVisit(response, request, &rastrillo.Ctx{}))
	}))
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if calls != 1 {
		t.Errorf("%s %s ran the callback %d times, want once: "+
			"the route did not match, or the assertions inside "+
			"the callback were skipped", method, path, calls)
	}
	return recorder
}

func TestRefusedAnswersEachKindAndReportsIt(t *testing.T) {
	cases := []struct {
		err    error
		status int
		answer bool
	}{
		{nil, 0, false},
		{action.BadRequest{Status: 400, Message: "Bad request."}, 400, true},
		{op.NotFound{}, 404, true},
		{op.Forbidden{Reason: "x"}, 403, true},
		{errors.New("disk"), 500, true},
	}
	for _, scenario := range cases {
		var answered bool
		recorder := serve(t, "", func(visit *action.Visit) {
			answered = visit.Refused(scenario.err)
		})
		if answered != scenario.answer {
			t.Errorf("Refused(%v) = %v, want %v",
				scenario.err, answered, scenario.answer)
		}
		// An untouched recorder reports 200, so "nothing written" is
		// a 200 with an empty body.
		want := scenario.status
		if want == 0 {
			want = http.StatusOK
		}
		if recorder.Code != want ||
			(scenario.status == 0 && recorder.Body.Len() != 0) {
			t.Errorf("Refused(%v) wrote %d %q, want %d",
				scenario.err, recorder.Code, recorder.Body, want)
		}
	}
}

func TestBadRequestIsAnsweredWithItsOwnWords(t *testing.T) {
	recorder := serve(t, "", func(visit *action.Visit) {
		visit.Refused(action.BadRequest{Status: 400, Message: "Bad request."})
	})
	body := strings.TrimSpace(recorder.Body.String())
	if body != "Bad request." {
		t.Errorf("body = %q, want Bad request.", body)
	}
}

func TestRefusedShowingShowsTheFormAgain(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		answer bool
		calls  int
		status int
		errors form.Errors
		code   int
	}{
		{"invalid", op.Invalid{Fields: map[string]string{"Name": "m"}},
			true, 1, 422, form.Errors{"Name": "m"}, 200},
		{"conflict", op.Conflict{Message: "moved"},
			true, 1, 409, form.Errors{action.WholeForm: "moved"}, 200},
		{"bad request",
			action.BadRequest{Status: 400, Message: "Bad request."},
			true, 0, 0, nil, 400},
		{"not found", op.NotFound{}, true, 0, 0, nil, 404},
		{"nothing", nil, false, 0, 0, nil, 200},
	}
	for _, scenario := range cases {
		again := &showingAgain{}
		var answered bool
		recorder := serve(t, "", func(visit *action.Visit) {
			answered = visit.RefusedShowing(scenario.err, again)
		})
		if answered != scenario.answer || again.calls != scenario.calls ||
			again.status != scenario.status ||
			!reflect.DeepEqual(again.errors, scenario.errors) ||
			recorder.Code != scenario.code {
			t.Errorf("%s: answered %v, ShowAgain %d times with %d %v, "+
				"wrote %d; want %v, %d times with %d %v, wrote %d",
				scenario.name, answered, again.calls, again.status,
				again.errors, recorder.Code, scenario.answer,
				scenario.calls, scenario.status, scenario.errors,
				scenario.code)
		}
	}
}

func TestRefusedShowingWithNoFormSaysUseRefused(t *testing.T) {
	var recovered any
	serve(t, "", func(visit *action.Visit) {
		defer func() { recovered = recover() }()
		invalid := op.Invalid{Fields: map[string]string{"A": "b"}}
		visit.RefusedShowing(invalid, nil)
	})
	if !strings.Contains(fmt.Sprint(recovered), "use Refused") {
		t.Errorf("recovered %v, want a panic saying use Refused", recovered)
	}
}

func TestABrokenBodyIsABadRequest(t *testing.T) {
	tooBig := strings.Repeat("a", 1<<20+1)
	want := action.BadRequest{Status: 400, Message: "Bad request."}
	serve(t, "x="+tooBig, func(visit *action.Visit) {
		if err := visit.ReadBody(); err != want {
			t.Errorf("ReadBody() = %v, want %v", err, want)
		}
	})
	serve(t, "x="+tooBig, func(visit *action.Visit) {
		parsed, err := visit.ReadForm(form.Field{Name: "x"})
		if err != want || parsed != nil {
			t.Errorf("ReadForm() = %v, %v, want nil, %v", parsed, err, want)
		}
	})
	serve(t, "x="+tooBig, func(visit *action.Visit) {
		first, second := visit.ReadBody(), visit.ReadBody()
		if first != want || second != want {
			t.Errorf("ReadBody twice = %v, %v, want %v both",
				first, second, want)
		}
	})
	serve(t, "x=%zz", func(visit *action.Visit) {
		if err := visit.ReadBody(); err != want {
			t.Errorf("ReadBody() of bad escapes = %v, want %v", err, want)
		}
	})
}

func TestAFieldThatDoesNotParseIsARefusal(t *testing.T) {
	fields := []form.Field{{Name: "Name", Kind: form.Text, Required: true}}

	serve(t, "Other=1", func(visit *action.Visit) {
		parsed, err := visit.ReadForm(fields...)
		var invalid op.Invalid
		if parsed == nil || !errors.As(err, &invalid) {
			t.Fatalf("ReadForm() = %v, %v, want a form and op.Invalid",
				parsed, err)
		}
		if _, named := invalid.Fields["Name"]; !named {
			t.Errorf("Invalid.Fields = %v, want Name", invalid.Fields)
		}
	})

	serve(t, "Name=Ada", func(visit *action.Visit) {
		parsed, err := visit.ReadForm(fields...)
		if parsed == nil || err != nil || parsed.String("Name") != "Ada" {
			t.Errorf("ReadForm() = %v, %v, want Ada, nil", parsed, err)
		}
	})
}

func TestRefusedLogsTheRouteItAnswered(t *testing.T) {
	var logged strings.Builder
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	router := chi.NewRouter()
	router.Post("/things/{id}", func(
		response http.ResponseWriter, request *http.Request,
	) {
		ctx := &rastrillo.Ctx{Logger: logger}
		action.NewVisit(response, request, ctx).Refused(errors.New("disk"))
	})
	request := httptest.NewRequest(http.MethodPost, "/things/7", nil)
	router.ServeHTTP(httptest.NewRecorder(), request)
	if !strings.Contains(logged.String(), "POST /things/{id}") {
		t.Errorf("log = %q, want POST /things/{id}", logged.String())
	}
}

// spy stands in for *testing.T so a test can watch a helper fail
// without failing itself.
type spy struct {
	testing.TB
	failures []string
}

func (watcher *spy) Helper() {}

func (watcher *spy) Errorf(format string, args ...any) {
	watcher.failures = append(watcher.failures, fmt.Sprintf(format, args...))
}

func (watcher *spy) Fatalf(format string, args ...any) {
	watcher.Errorf(format, args...)
}

func TestServeFailsTheTestWhenItsCallbackDoesNotRun(t *testing.T) {
	watcher := &spy{TB: t}
	serveAt(watcher, http.MethodPost, "/elsewhere", "text/plain", "",
		func(visit *action.Visit) {})
	if len(watcher.failures) == 0 {
		t.Error("serveAt passed though its callback never ran")
	}
}

func TestServeAcceptsACallbackThatRunsOnce(t *testing.T) {
	watcher := &spy{TB: t}
	serve(watcher, "", func(visit *action.Visit) {})
	if len(watcher.failures) != 0 {
		t.Errorf("serve failed a callback that ran once: %v",
			watcher.failures)
	}
}

func TestAPointerBadRequestIsABadRequestToo(t *testing.T) {
	pointer := &action.BadRequest{Status: 400, Message: "Bad request."}
	cases := map[string]error{
		"direct":  pointer,
		"wrapped": fmt.Errorf("reading the form: %w", pointer),
	}
	for name, refusal := range cases {
		recorder := serve(t, "", func(visit *action.Visit) {
			if !visit.Refused(refusal) {
				t.Errorf("%s: Refused reported nothing answered", name)
			}
		})
		body := strings.TrimSpace(recorder.Body.String())
		if recorder.Code != 400 || body != "Bad request." {
			t.Errorf("%s: answered %d %q, want 400 %q",
				name, recorder.Code, body, "Bad request.")
		}
	}
}

func TestAWrappedBadRequestValueIsStillABadRequest(t *testing.T) {
	refusal := fmt.Errorf("reading: %w",
		action.BadRequest{Status: 400, Message: "Bad request."})
	recorder := serve(t, "", func(visit *action.Visit) {
		visit.Refused(refusal)
	})
	if recorder.Code != 400 {
		t.Errorf("answered %d, want 400", recorder.Code)
	}
}
