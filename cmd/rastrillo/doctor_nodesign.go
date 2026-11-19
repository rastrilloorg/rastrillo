package main

import (
	"fmt"
	"io"
	"path/filepath"
)

// doctorNoDesignMsg is the line doctor prints for an app with no
// design system, such as a bare app scaffolded without it.
const doctorNoDesignMsg = "No design system here (no static files or " +
	"templates), so there is nothing to compare."

// withoutDesignSystem reports whether the app at dir, which has no
// static files, has no layout either. That is an app without a design
// system (bare's --design-system=false), not a broken one, and failing
// on it stopped even --probe's locale check. A layout with nothing
// vendored is still the error: its pages link files that are gone.
func withoutDesignSystem(dir string) bool {
	layouts, _ := filepath.Glob(filepath.Join(dir, "internal", "*",
		"templates", "layout.html"))
	return layouts == nil
}

func printNoDesign(output io.Writer, module string) {
	fmt.Fprintf(output, "rastrillo doctor: %s\n  %s\n", module,
		doctorNoDesignMsg)
}
