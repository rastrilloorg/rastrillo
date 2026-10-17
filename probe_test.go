package rastrillo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/checkconfig"
	"amadan.net/rastrillo/rastrillo/locales"
)

// exited is what the test exit function panics with, so run stops at
// the point a real process would have exited.
type exited struct{}

// callRun runs run with the given environment and argv, recording the
// exit code instead of exiting. code is -1 when run returned without
// exiting.
func callRun(t *testing.T, opts Options, args []string, env map[string]string) (code int, stdout string, err error) {
	t.Helper()
	code = -1
	var out strings.Builder
	func() {
		defer func() {
			if v := recover(); v != nil {
				if _, ok := v.(exited); !ok {
					panic(v)
				}
			}
		}()
		err = run(opts, args, func(k string) string { return env[k] }, &out, io.Discard, func(c int) { code = c; panic(exited{}) })
	}()
	return code, out.String(), err
}

// The probe is the first statement of Run, before the sidecar
// dispatch: a sidecar invocation reaches the app's Options.Sidecar work
// without passing Resolve or Serve, so a probe placed any later would
// let an inherited variable start background work against a live
// database (spec §3, confirmation round gap 1).
func TestProbeRunsBeforeSidecarDispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"serve"}, {"-db", "x.db"}, {"sidecar", "run"}} {
		opts := Options{
			// Stops run as an exit would: returning would put the
			// sidecar loop to sleep and hang the test instead of failing it.
			Sidecar: func(context.Context) (time.Time, error) {
				t.Errorf("%v: Options.Sidecar ran under the probe", args)
				panic(exited{})
			},
			Mux: http.NewServeMux(),
		}
		code, out, _ := callRun(t, opts, args, map[string]string{checkconfig.Env: "1"})
		if code != 0 || !strings.Contains(out, `"rastrillo_check_config":1`) {
			t.Errorf("%v: exit %d, stdout %q; want the report and exit 0", args, code, out)
		}
	}
}

// Without the variable the dispatch is what it was before the probe:
// `sidecar run` takes the sidecar path, anything else resolves the argv
// and serves. Each path is told apart by the error it stops on, so no
// test listens or loops: a nil Options.Sidecar is the sidecar loop's own
// refusal, and an unusable Addr is Serve's listen failing after Handler
// built everything.
func TestDispatchUnchangedWithoutProbe(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "rastrillo: listen:"},
		{[]string{"serve"}, "rastrillo: listen:"},
		{[]string{"sidecar", "run"}, "Options.Sidecar is nil"},
		// A lone "sidecar" is not the sidecar shape: the flag parsing
		// rejects it rather than the sidecar path absorbing it.
		{[]string{"sidecar"}, `unexpected argument "sidecar"`},
	} {
		opts := Options{Mux: http.NewServeMux(), Addr: "127.0.0.1:-1"}
		code, out, err := callRun(t, opts, c.args, nil)
		if code != -1 || out != "" {
			t.Errorf("%v: exit %d, stdout %q; the probe ran with the variable unset", c.args, code, out)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err %v; want one containing %q", c.args, err, c.want)
		}
	}
}

func TestProbeValueOtherThanOneExitsTwo(t *testing.T) {
	code, out, _ := callRun(t, Options{Mux: http.NewServeMux()}, nil, map[string]string{checkconfig.Env: "0"})
	if code != 2 || out != "" {
		t.Errorf("RASTRILLO_CHECK_CONFIG=0: exit %d, stdout %q; want 2 and nothing: a stray 0 is an error, not a silent check", code, out)
	}
}

func TestProbeReportsProblemsAndExitsOne(t *testing.T) {
	code, out, _ := callRun(t, Options{Mux: http.NewServeMux(), DefaultLocale: "de--CH"}, nil, map[string]string{checkconfig.Env: "1"})
	if code != 1 || !strings.Contains(out, `"ok":false`) {
		t.Errorf("exit %d, %s", code, out)
	}
}

// probe is what Resolve and ProbeConfig run first. With the variable
// set it must never return, or the caller would go on to open its
// database; unset, it must return false and print nothing.
func TestProbeExitsOrReturns(t *testing.T) {
	var code int
	var out strings.Builder
	exit := func(c int) { code = c; panic(exited{}) }
	getenv := func(k string) string { return map[string]string{checkconfig.Env: "1"}[k] }
	func() {
		defer func() {
			if v := recover(); v != nil {
				if _, ok := v.(exited); !ok {
					panic(v)
				}
			}
		}()
		probe(Options{DefaultLocale: "en"}, getenv, &out, io.Discard, exit)
		t.Error("probe returned with the variable set")
	}()
	if code != 0 || !strings.Contains(out.String(), `"ok":true`) {
		t.Errorf("exit %d, %s", code, out.String())
	}
	if probe(Options{}, func(string) string { return "" }, &out, io.Discard, exit) {
		t.Error("probe ran with the variable unset")
	}
}

// FormatLocale is the one seam the generated actions format through,
// so its three answers are pinned: an offered identity formats in the
// locale CheckConfig chose for it (zh-TW in zh-Hant, a pack this test
// binary does not link, so only the installed mapping can answer); a
// request that never went through Middleware, a monolingual app's,
// formats in the installed default; and the identity itself is never
// the answer when it differs from its formatting locale.
func TestFormatLocale(t *testing.T) {
	defer locales.SetDefaultForTest("")()
	// The mapping CheckConfig would produce for en and zh-TW, handed to
	// Install directly because CheckConfig refuses zh-TW here, where
	// the zh pack is not linked.
	locales.Install(locales.Config{Default: "en", DefaultFormat: "en", Offered: []string{"en", "zh-TW"},
		Formats: map[string]string{"en": "en", "zh-TW": "zh-Hant"}})
	l, err := NewLocales([]string{"en", "zh-TW"}, "en", Catalog{}, testFS())
	if err != nil {
		t.Fatal(err)
	}
	var got string
	h := l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = FormatLocale(r) }))
	for path, want := range map[string]string{"/zh-TW/orders": "zh-Hant", "/en/orders": "en"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
		if got != want {
			t.Errorf("FormatLocale for %s = %q, want %q", path, got, want)
		}
	}
	if got := FormatLocale(httptest.NewRequest("GET", "/orders", nil)); got != "en" {
		t.Errorf("FormatLocale outside Middleware = %q, want the installed default en", got)
	}
}
