// Command tmplgen-corpus regenerates
// tmpl/internal/corpus/rastrillo_templates_gen.go. Run it from the
// repository root after changing a corpus case:
//
//	go run ./internal/tmplgen/cmd/tmplgen-corpus
package main

import (
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/internal/tmplgen"
)

func main() {
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Fprintln(os.Stderr,
			"tmplgen-corpus: run this from the repository root")
		os.Exit(2)
	}
	res, err := tmplgen.Run(".", tmplgen.Options{
		Mains: []string{"./tmpl/internal/corpus/cmd/corpuscapture"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tmplgen-corpus:", err)
		os.Exit(1)
	}
	report(res)
	if len(res.CaptureFailures) > 0 {
		os.Exit(1)
	}
}

// report prints what the run wrote, removed and left uncompiled, and
// which mains it could not capture.
func report(res tmplgen.Result) {
	for _, path := range res.Written {
		fmt.Println("wrote", path)
	}
	for _, path := range res.Removed {
		fmt.Println("removed", path)
	}
	for _, note := range res.Unsupported {
		fmt.Printf("not compiled: %s %q: %s\n",
			note.Set, note.Template, note.Reason)
	}
	for _, failure := range res.CaptureFailures {
		fmt.Fprintf(os.Stderr, "tmplgen-corpus: %s: %s\n",
			failure.Owner, failure.Reason)
	}
}
