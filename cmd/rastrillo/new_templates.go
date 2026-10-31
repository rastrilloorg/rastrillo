package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// newTemplates compiles a new app's templates, so the app starts with
// the file generate --check requires. A variable so that this
// package's tests, which scaffold dozens of apps to read their files,
// can leave it out.
var newTemplates = compileNewTemplates

// newTemplatesLater is printed when new cannot compile the templates:
// the app works, on html/template, and its gate wants the file.
const newTemplatesLater = "templates not compiled: %s; after go mod tidy, " +
	"run rastrillo generate"

// compileNewTemplates runs go mod tidy in the app at dir, because
// capture builds the app, and then generate's template step. Neither
// fails new: offline, or with a rastrillo not yet published, the app
// is complete without the file, and the line says how to get it.
func compileNewTemplates(dir string) {
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		fmt.Printf(newTemplatesLater+"\n",
			"go mod tidy failed: "+tidyFailure(err, out))
		return
	}
	result, err := compileTemplates(dir, false)
	if err != nil {
		fmt.Printf(newTemplatesLater+"\n", err)
	} else if len(result.CaptureFailures) > 0 {
		fmt.Printf(newTemplatesLater+"\n",
			"the app did not start in capture mode")
	}
}

// tidyFailure says why go mod tidy failed. Its last line names the
// module it could not get; the rest is every import that needed it.
func tidyFailure(err error, output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if last := lines[len(lines)-1]; last != "" {
		return strings.TrimSpace(last)
	}
	return err.Error()
}
