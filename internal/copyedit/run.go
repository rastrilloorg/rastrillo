//go:build ignore

// run applies one copyedit edit file to the tree:
//
//	GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit edit.json
//
// It is a //go:build ignore program rather than a command package
// because it runs only when approved copy lands, and as a command it
// would add a binary to the module that no app or release uses. The
// work is copyedit.Run, so the all-or-nothing write is tested there.
package main

import (
	"flag"
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/internal/copyedit"
)

func main() {
	path := flag.String("edit", "", "the edit file")
	flag.Parse()
	if err := copyedit.Run(*path, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "copyedit:", err)
		os.Exit(1)
	}
}
