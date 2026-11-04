package main

import (
	"fmt"
	"io"
)

// printFile writes one file's line of the report and, for a file --fix
// leaves for its pair, why.
func (findings *report) printFile(output io.Writer, file vendoredFile) {
	line := func(format string, arguments ...any) {
		fmt.Fprintf(output, format+"\n", arguments...)
	}
	switch file.state {
	case fileOK:
		line("  ok       %s", file.name)
	case fileMine:
		line("  yours    %s (recorded as a deliberate edit in %s)",
			file.name, findings.pinFile)
	case fileRemoved:
		line("  yours    %s (removed on purpose, recorded in %s)",
			file.name, findings.pinFile)
	case fileUnknownTheme:
		line("  yours    %s (%s — a theme of your own is supported, "+
			"so nothing is compared)", file.name, findings.themeFrom)
	case fileAbsent:
		line("  absent   %s (the library ships %s; deleting one you do "+
			"not use is supported)", file.name, size(len(file.lib)))
	case fileUnreadable:
		line("  error    %s: %v", file.name, file.err)
	case fileDiffers:
		line("  drift    %s", file.name)
		for _, difference := range describeDiff(file.app, file.lib) {
			line("             %s", difference)
		}
	}
	if partner := findings.heldBy(file, false); partner != "" {
		line("             --fix leaves it: %s",
			findings.pairNote(file.name, partner))
	}
}

// fixSummary closes what --fix printed: the layouts its files leave
// without fiesta.js, then what it wrote.
func (findings *report) fixSummary(
	output io.Writer, force bool, wrote, skipped int,
) error {
	fmt.Fprintln(output)
	findings.adviseFiestaJS(output, force)
	if wrote == 0 {
		fmt.Fprintf(output, "Nothing re-copied (%s left alone).\n",
			plural(skipped, "file"))
		if skipped > 0 {
			return nil
		}
		return findings.exit()
	}
	fmt.Fprintf(output, "Re-copied %s from %s — still yours, so read the "+
		"diff before you commit it.\n", plural(wrote, "file"),
		findings.cliVersion)
	return nil
}
