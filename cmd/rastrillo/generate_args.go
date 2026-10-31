package main

import (
	"errors"
	"flag"
	"path/filepath"
)

// generateArgs is a parsed `rastrillo generate [flags] [dir]`, with dir
// made absolute.
type generateArgs struct {
	check, templates, acceptBreaking bool
	dir                              string
}

// The generate flags' usage lines.
const (
	checkUsage = "verify without writing (route collisions, " +
		"action build tags, i18n catalog completeness, " +
		"hand-written code on its operation, compiled templates)"
	templatesUsage = "compile the app's templates to Go; " +
		"set it to false to leave them on html/template, " +
		"as rastrillo dev does"
	acceptBreakingUsage = "rewrite api/v1.json over a change that " +
		"breaks /api/v1 clients; without it, generate refuses and " +
		"leaves the file as it was"
)

// parseGenerateArgs reads generate's command line, flags before the
// directory (see runGenerate).
func parseGenerateArgs(args []string) (generateArgs, error) {
	var parsed generateArgs
	fset := flag.NewFlagSet("generate", flag.ContinueOnError)
	fset.BoolVar(&parsed.check, "check", false, checkUsage)
	fset.BoolVar(&parsed.templates, "templates", true, templatesUsage)
	fset.BoolVar(&parsed.acceptBreaking, "accept-breaking", false,
		acceptBreakingUsage)
	// Kept only to refuse: an undefined flag fails with Go's generic
	// "flag provided but not defined", which names no way out.
	fset.String("default-locale", "", defaultLocaleRemoved)
	if err := fset.Parse(args); err != nil {
		return parsed, err
	}
	removed := false
	fset.Visit(func(given *flag.Flag) {
		removed = removed || given.Name == "default-locale"
	})
	if removed {
		return parsed, errors.New(defaultLocaleRemoved)
	}

	dir := "."
	if rest := fset.Args(); len(rest) > 0 {
		dir = rest[0]
	}
	dir, err := filepath.Abs(dir)
	parsed.dir = dir
	return parsed, err
}
