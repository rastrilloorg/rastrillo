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

	newDesignSystemFlagMsg  = "write the design system: templates, styles, scripts and icons (only bare can turn it off)"
	newDesignSystemShapeMsg = "--design-system=false works with --scaffold bare only: single and accounts build their pages from the design system"
	newDesignFlagOffMsg     = "--%s does nothing with --design-system=false: leave it out"

	newAccountsAuthNoneMsg = "--auth=none does nothing with --scaffold accounts, which is the app with sign-in: use --scaffold single for an app without it"
	newBareAuthMsg         = "--auth=%s needs a database: use --scaffold single or accounts"
)

// checkAuth refuses an --auth the shape cannot honour. bare has no
// database for sessions and links, so its sign-in could not run.
// accounts is the app with sign-in, so a typed --auth=none would be
// silently overridden; o.Set tells it from --auth left at the flag's
// default, which stays none for single.
func checkAuth(shapeName string, o newOpts) error {
	switch {
	case shapeName == "bare" && o.Auth != "none":
		return fmt.Errorf(newBareAuthMsg, o.Auth)
	case shapeName == "accounts" && o.Set["auth"] && o.Auth == "none":
		return errors.New(newAccountsAuthNoneMsg)
	}
	return nil
}

// writeAccounts is single with sign-in under the menu's name, so the two
// cannot drift into separate sign-in scaffolds kept in step by hand.
func writeAccounts(o newOpts) error {
	if !o.Set["auth"] {
		o.Auth = "magic"
	}
	return writeSingle(o)
}

// designFlags only choose design-system files, so with it off each one
// typed would be silently ignored.
var designFlags = []string{"icons", "icon-delivery", "theme", "shell"}

// checkDesignSystem refuses --design-system=false where it cannot hold:
// single and accounts render their token, sign-in and error pages
// through it. o.Set, not the values: --theme=day typed out is a choice
// the app would never get.
func checkDesignSystem(shapeName string, o newOpts) error {
	if o.DesignSystem {
		return nil
	}
	if shapeName != "bare" {
		return errors.New(newDesignSystemShapeMsg)
	}
	for _, f := range designFlags {
		if o.Set[f] {
			return fmt.Errorf(newDesignFlagOffMsg, f)
		}
	}
	return nil
}

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

// shapes is the menu in the order the help names it. Reserved names
// stay on it so lookupShape can tell "planned" from "mistyped".
var shapes = []shape{
	{Name: "bare", Summary: "a page, no database", Write: writeBare},
	{Name: defaultShape, Summary: "one app and its SQLite data", Write: writeSingle},
	{Name: "accounts", Summary: "single with sign-in by emailed link", Write: writeAccounts},
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
