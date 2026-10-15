package locales_test

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

func TestProbeOutputFields(t *testing.T) {
	var out, errOut strings.Builder
	run, code := locales.Probe("1", "en", nil, &out, &errOut)
	if !run || code != 0 {
		t.Fatalf("Probe(1) = %v, %d", run, code)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out.String()), &m); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := "default default_format formats offered ok packs_linked packs_required problems rastrillo_check_config"
	if strings.Join(keys, " ") != want {
		t.Errorf("probe fields %v, want %s: doctor and scripts read these by name", keys, want)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("probe wrote %q; want exactly one JSON line on stdout", out.String())
	}
	if !strings.Contains(errOut.String(), "exited without serving") {
		t.Errorf("stderr %q does not say it exited without serving", errOut.String())
	}
	for _, env := range []string{"0", "true", "yes", " 1"} {
		if run, code := locales.Probe(env, "en", nil, io.Discard, io.Discard); !run || code != 2 {
			t.Errorf("Probe(%q) = %v, %d; want run and exit 2: only the exact value 1 checks", env, run, code)
		}
	}
}
