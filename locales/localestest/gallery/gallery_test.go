package gallery

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/bn"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
	_ "amadan.net/rastrillo/rastrillo/locales/ga"
	_ "amadan.net/rastrillo/rastrillo/locales/hi"
	_ "amadan.net/rastrillo/rastrillo/locales/ja"
	_ "amadan.net/rastrillo/rastrillo/locales/pt"
	_ "amadan.net/rastrillo/rastrillo/locales/ru"
	_ "amadan.net/rastrillo/rastrillo/locales/vi"
	_ "amadan.net/rastrillo/rastrillo/locales/yue"
	_ "amadan.net/rastrillo/rastrillo/locales/zh"
)

func TestDefaultPacksOnly(t *testing.T) {
	if got := len(locales.LinkedPacks()); got != 12 {
		t.Fatalf("linked %d packs, want 12: %v", got, locales.LinkedPacks())
	}
	for _, c := range []struct {
		tag                 string
		id, wanted, missing string
	}{
		{"es-AR", "es-AR", "es-AR", ""}, // a regional variant of a linked language
		{"pt-PT", "pt-PT", "pt-PT", ""},
		{"en-IE", "en-IE", "en-IE", ""},
		{"de-AT", "root", "de-AT", "de"}, // pack not linked: root, and the pack is named
		{"fr-CH", "root", "fr-CH", "fr"},
		{"hi-Latn", "hi-Latn", "hi-Latn", ""}, // stored whole: needs no en-IN
		{"xx", "root", "", ""},                // no pack carries it
	} {
		r := locales.Resolve(c.tag)
		if r.ID != c.id || r.Wanted != c.wanted || r.MissingPack != c.missing {
			t.Errorf("Resolve(%q) = %+v, want ID %q Wanted %q MissingPack %q", c.tag, r, c.id, c.wanted, c.missing)
		}
	}
	// The currency still comes from the data: an unlinked locale changes
	// how an amount is written, never which currency it is in.
	got, err := currency.Format("de-AT", 128450, "EUR")
	if err != nil || got != "€\u00a01,284.50" {
		t.Errorf("Format(de-AT) without the de pack = %q, %v", got, err)
	}
}

// An unlinked pack at request time is never silent, and never noisy:
// one warning per missing pack per process, however many requests ask.
// Run in a fresh process: the once-per-process state is real state,
// and earlier tests in this binary have already consumed it.
func TestMissingPackWarnsOncePerPack(t *testing.T) {
	if os.Getenv("WARN_ONCE_CHILD") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMissingPackWarnsOncePerPack$", "-test.count=1")
		cmd.Env = append(os.Environ(), "WARN_ONCE_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
		return
	}
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	for i := 0; i < 3; i++ {
		currency.Format("de-AT", 128450, "EUR")
		currency.Format("de-CH", 128450, "CHF")
		currency.Format("fr-CH", 128450, "CHF")
	}
	if n := strings.Count(buf.String(), "the de pack"); n != 1 {
		t.Errorf("de warned %d times, want once:\n%s", n, buf.String())
	}
	if n := strings.Count(buf.String(), "the fr pack"); n != 1 {
		t.Errorf("fr warned %d times, want once:\n%s", n, buf.String())
	}
}

// The two CheckConfig cases that need a pack to be missing: in the
// locales package every pack is linked, so there they cannot fail.
func TestCheckConfigNamesTheMissingPack(t *testing.T) {
	for _, c := range []struct {
		def     string
		offered []string
		wantErr string
	}{
		{"de", nil, "needs the de pack"},
		{"en", []string{"en", "fr-CH"}, "needs the fr pack"},
	} {
		if _, err := locales.CheckConfig(c.def, c.offered); err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("CheckConfig(%q, %v) error = %v, want %q", c.def, c.offered, err, c.wantErr)
		}
	}
}

// Round three, finding 10: past every linked parent, a tag formats in
// the app's default locale, as the page's words do; root only without
// an app.
func TestDefaultFallback(t *testing.T) {
	restore := locales.SetDefaultForTest("")
	defer restore()
	before, _ := currency.Format("de-AT", 128450, "EUR")
	locales.SetDefault("en")
	r := locales.Resolve("de-AT")
	if r.ID != "en" || !r.UsedDefault || r.MissingPack != "de" {
		t.Errorf("Resolve(de-AT) with default en = %+v", r)
	}
	after, _ := currency.Format("de-AT", 128450, "EUR")
	if before != "\u20ac\u00a01,284.50" || after != "\u20ac1,284.50" {
		t.Errorf("de-AT without its pack: %q before SetDefault (root), %q after (en)", before, after)
	}
}

// Round four, finding 1: the probe's fixed fields and exit codes.
func TestProbe(t *testing.T) {
	var out, errOut strings.Builder
	if run, _ := locales.Probe("", "en", nil, &out, &errOut); run || out.Len() > 0 {
		t.Error("probe ran with the variable unset")
	}
	if run, code := locales.Probe("yes", "en", nil, &out, &errOut); !run || code != 2 || out.Len() > 0 {
		t.Errorf("RASTRILLO_CHECK_CONFIG=yes: run %v, code %d, out %q", run, code, out.String())
	}
	out.Reset()
	run, code := locales.Probe("1", "zh-TW", []string{"zh-TW", "en"}, &out, &errOut)
	want := `{"rastrillo_check_config":1,"ok":true,"default":"zh-TW","default_format":"zh-Hant","offered":["zh-TW","en"],"formats":{"en":"en","zh-TW":"zh-Hant"},"packs_required":["en","zh"],"packs_linked":["ar","bn","en","es","ga","hi","ja","pt","ru","vi","yue","zh"],"problems":[]}` + "\n"
	if !run || code != 0 || out.String() != want {
		t.Errorf("probe ok: code %d\n got %s\nwant %s", code, out.String(), want)
	}
	out.Reset()
	if _, code := locales.Probe("1", "de", nil, &out, &errOut); code != 1 || !strings.Contains(out.String(), `"ok":false`) || !strings.Contains(out.String(), "needs the de pack") {
		t.Errorf("probe with a missing pack: code %d, %s", code, out.String())
	}
}

// Review focus 2: a stored preference or a cookie names an identity the
// app does not offer. FormatFor must never answer root inside an app:
// the amount would be written in conventions the page around it does
// not use.
func TestFormatForUnofferedIdentity(t *testing.T) {
	cfg, err := locales.CheckConfig("en", []string{"en", "zh-TW"})
	if err != nil {
		t.Fatal(err)
	}
	restore := locales.SetDefaultForTest("")
	defer restore()
	locales.Install(cfg)
	for identity, want := range map[string]string{
		"en":    "en",
		"zh-TW": "zh-Hant", // offered: CheckConfig's format, not the identity
		"es-MX": "es-MX",   // not offered but linked: its own conventions
		"de-CH": "en",      // its pack is not linked: the app default, never root
		"xx":    "en",      // no pack carries it
		"":      "en",
	} {
		if got := locales.FormatFor(identity); got != want {
			t.Errorf("FormatFor(%q) = %q, want %q", identity, got, want)
		}
	}
}
