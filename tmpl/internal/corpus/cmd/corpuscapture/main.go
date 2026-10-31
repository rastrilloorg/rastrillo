// Command corpuscapture builds every corpus set and writes them where
// tmpl.CaptureEnv says, for go run ./internal/tmplgen/cmd/tmplgen-corpus.
// It is the corpus's stand-in for an app's main, which writes its
// capture from rastrillo.Serve.
package main

import (
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/tmpl"
	"amadan.net/rastrillo/rastrillo/tmpl/internal/corpus"
)

func main() {
	path := os.Getenv(tmpl.CaptureEnv)
	if path == "" {
		fmt.Fprintf(
			os.Stderr,
			"corpuscapture: set %s to the file to write\n",
			tmpl.CaptureEnv,
		)
		os.Exit(2)
	}
	for _, testCase := range corpus.Cases() {
		if _, err := testCase.Build(); err != nil {
			fmt.Fprintf(
				os.Stderr,
				"corpuscapture: %s: %v\n",
				testCase.Name,
				err,
			)
			os.Exit(1)
		}
	}
	for _, testCase := range corpus.PagesCases() {
		if _, err := testCase.Build(); err != nil {
			fmt.Fprintf(
				os.Stderr,
				"corpuscapture: %s: %v\n",
				testCase.Name,
				err,
			)
			os.Exit(1)
		}
	}
	if err := tmpl.WriteCapture(path); err != nil {
		fmt.Fprintln(os.Stderr, "corpuscapture:", err)
		os.Exit(1)
	}
}
