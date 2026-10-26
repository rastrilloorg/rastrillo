package main

import (
	"errors"
	"fmt"
)

// The copy `rastrillo new` shows about --scaffold. One constant each, so
// the wording that comes back from copy review is one edit.
const (
	newScaffoldFlagMsg  = "what to scaffold: bare (a page, no database), single (the default: one app and its SQLite data) or accounts (single with sign-in by emailed link)"
	newUnknownShapeMsg  = "unknown --scaffold %q: choose bare, single or accounts"
	newReservedShapeMsg = "--scaffold %s is not built yet: it is %s. Use bare, single or accounts for now"
	newReservedHomeMsg  = "Plan 6 of the user-models roadmap"
	newReservedHubMsg   = "Plan 7 of the user-models roadmap"
)

const defaultShape = "single"

// newOpts is what `rastrillo new` was asked for, after flag parsing and
// before any choice is checked, so every shape resolves the same flags
// the same way.
type newOpts struct {
	Name, Pkg, Locale, IconSet, IconDelivery, UX, Theme, Shell, Auth string
	DesignSystem                                                     bool
	// Set is the flags the user passed rather than left at their
	// default: a flag a shape ignores must be refused by name, and only
	// Set can tell a typed --theme=day from the default one.
	Set map[string]bool
}

// shape is one entry on the `rastrillo new --scaffold` menu. A shape
// with Reserved set holds its name and nothing else: it is refused
// with the sentence that says what it waits for.
type shape struct {
	Name, Summary, Reserved string
	Write                   func(newOpts) error
}

var errShapeNotBuilt = errors.New("not built yet")

// shapes is the menu in the order the help names it. Reserved names
// stay on it so lookupShape can tell "planned" from "mistyped".
var shapes = []shape{
	{Name: "bare", Summary: "a page, no database", Write: func(newOpts) error { return errShapeNotBuilt }},
	{Name: defaultShape, Summary: "one app and its SQLite data", Write: writeSingle},
	{Name: "accounts", Summary: "single with sign-in by emailed link", Write: func(newOpts) error { return errShapeNotBuilt }},
	{Name: "home", Reserved: newReservedHomeMsg},
	{Name: "hub", Reserved: newReservedHubMsg},
}

// shapeNames is the shapes someone can scaffold today, in menu order.
func shapeNames() []string {
	var names []string
	for _, s := range shapes {
		if s.Reserved == "" {
			names = append(names, s.Name)
		}
	}
	return names
}

func lookupShape(name string) (shape, error) {
	for _, s := range shapes {
		if s.Name != name {
			continue
		}
		if s.Reserved != "" {
			return shape{}, fmt.Errorf(newReservedShapeMsg, name, s.Reserved)
		}
		return s, nil
	}
	return shape{}, fmt.Errorf(newUnknownShapeMsg, name)
}
