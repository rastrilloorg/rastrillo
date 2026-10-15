// Command fetch downloads the pinned cldr-json inputs and checks or
// writes their hash manifest.
//
//	go run ./cmd/fetch [-dir DIR] [-write]
//
// Without -write it fails when any file differs from
// sources/inputs.sha256. -write records the manifest instead, which
// is only right when the version itself is being changed.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo/hack/moneycldr/cldrsrc"
)

func main() {
	dir := flag.String("dir", cldrsrc.DefaultDir(), "cache directory for the cldr-json files")
	write := flag.Bool("write", false, "write the hash manifest instead of checking it")
	flag.Parse()
	manifest := filepath.Join("sources", "inputs.sha256")

	files, err := cldrsrc.Fetch(*dir)
	if err != nil {
		fail(err)
	}
	if *write {
		m, err := cldrsrc.Manifest(*dir, files)
		if err != nil {
			fail(err)
		}
		if err := os.MkdirAll("sources", 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(manifest, m, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s: %d files\n", manifest, len(files))
		return
	}
	checked, err := cldrsrc.Verify(*dir, manifest)
	if err != nil {
		fail(err)
	}
	if len(checked) != len(files) {
		fail(fmt.Errorf("%s lists %d files, the fetch needed %d", manifest, len(checked), len(files)))
	}
	fmt.Printf("%d files match %s\n", len(checked), manifest)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "fetch:", err)
	os.Exit(1)
}
