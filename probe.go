package rastrillo

import (
	"io"
	"net/http"
	"os"

	"amadan.net/rastrillo/rastrillo/internal/checkconfig"
	"amadan.net/rastrillo/rastrillo/locales"
)

// ProbeConfig runs the RASTRILLO_CHECK_CONFIG probe for opts'
// DefaultLocale and Locales. With the variable set it prints the
// report and exits the process, without opening or serving anything;
// unset, it returns. Run and Resolve call it first themselves; an app
// that calls neither makes it the first line of main, because the
// database entry points refuse to open while the variable is set.
func ProbeConfig(opts Options) {
	probe(opts, os.Getenv, os.Stdout, os.Stderr, os.Exit)
}

// probe is ProbeConfig with the process injected, so tests can record
// the exit code. It reports whether the probe ran; a real exit never
// returns, so true only reaches a test whose exit function returns.
func probe(opts Options, getenv func(string) string, stdout, stderr io.Writer, exit func(int)) bool {
	ran, code := locales.Probe(getenv(checkconfig.Env), opts.DefaultLocale, opts.Locales, stdout, stderr)
	if ran {
		exit(code)
	}
	return ran
}

// FormatLocale is the formatting locale for r: the linked locale whose
// conventions write its amounts, which generated actions pass to
// currency.Format. It is not LocaleFrom(r): an offered identity such as
// zh-TW is formatted by zh-Hant, and a monolingual app's requests carry
// no locale at all and format in the installed default.
func FormatLocale(r *http.Request) string {
	return locales.FormatFor(LocaleFrom(r))
}
