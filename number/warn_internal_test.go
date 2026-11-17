package number

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
)

// A locale whose pack the binary does not link is written in another
// locale's marks, so the operator hears about it once, as for money.
func TestNumberWarnsOnceAboutAMissingPack(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)
	warned.Delete("xx")
	defer warned.Delete("xx")
	missing := locales.Resolution{ID: "en", Wanted: "xx-YY",
		MissingPack: "xx"}
	warnMissing(missing)
	warnMissing(missing)
	warnMissing(locales.Resolution{ID: "en"})
	count := strings.Count(logged.String(), "needs the xx pack")
	if count != 1 || !strings.Contains(logged.String(), "locales/xx") {
		t.Errorf("warned %d times, want once naming the import: %s",
			count, logged.String())
	}
}
