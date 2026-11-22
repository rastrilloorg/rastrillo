package main

import (
	"errors"
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/iconsets"
	"amadan.net/rastrillo/rastrillo/ui"
)

// The copy `rastrillo new` shows about --scaffold. One constant each, so
// the wording that comes back from copy review is one edit.
const (
	newScaffoldFlagMsg = "what to scaffold: bare (a page, no database), " +
		"single (the default: one app and its SQLite data) or accounts " +
		"(single with sign-in by emailed link)"
	newUnknownShapeMsg = "unknown --scaffold %q: choose bare, single " +
		"or accounts"
	newReservedShapeMsg = "--scaffold %s is not built yet: it is %s. " +
		"Use bare, single or accounts for now"
	newReservedHomeMsg = "Plan 4 of the multi-instance apps roadmap"
	newReservedHubMsg  = "Plan 6 of the multi-instance apps roadmap"

	newAuthFlagMsg = "signin starts with an emailed link and lets " +
		"people add a passkey, authenticator app or passphrase; magic " +
		"is its old name; none gives no sign-in"

	newDesignSystemFlagMsg = "write the design system: templates, " +
		"styles, scripts and icons (only bare can turn it off)"
	newDesignSystemShapeMsg = "--design-system=false works with " +
		"--scaffold bare only: single and accounts build their pages " +
		"from the design system"
	newDesignFlagOffMsg = "--%s does nothing with " +
		"--design-system=false: leave it out"

	newAccountsAuthNoneMsg = "--auth=none does nothing with --scaffold " +
		"accounts, which is the app with sign-in: use --scaffold single " +
		"for an app without it"
	newBareAuthMsg = "--auth=%s needs a database: use --scaffold single " +
		"or accounts"
)

// newUsage is the error a run with no name, or two, gets.
func newUsage() error {
	return fmt.Errorf("usage: rastrillo new [--scaffold=%s] "+
		"[--design-system=false] [--icons=%s] [--icon-delivery=%s] "+
		"[--ux=%s] [--theme=%s] [--shell=%s] [--locale=%s] [--auth=%s] "+
		"<name>",
		strings.Join(shapeNames(), "|"),
		strings.Join(iconsets.Names(), "|"),
		strings.Join(iconsets.Deliveries(), "|"),
		strings.Join(profileNames(), "|"),
		strings.Join(ui.ThemeNames(), "|"),
		strings.Join(ui.LayoutNames(), "|"),
		strings.Join(rastrillo.BaseLocales(), "|"),
		strings.Join(authKinds, "|"))
}

// checkShape is every refusal that depends on the shape, run before
// the shape's Write so a refused run leaves the directory clean.
func checkShape(shapeName string, options newOpts) error {
	if err := checkDesignSystem(shapeName, options); err != nil {
		return err
	}
	if err := checkAuth(shapeName, options); err != nil {
		return err
	}
	if err := checkChoices(options); err != nil {
		return err
	}
	// Set, not the value: --ux=considered typed out is still a flag that
	// would silently do nothing.
	if shapeName == "bare" && options.Set["ux"] {
		return errors.New(newUXBareMsg)
	}
	return nil
}

// checkAuth refuses an --auth the shape cannot honour. bare has no
// database for sessions and links, so its sign-in could not run.
// accounts is the app with sign-in, so a typed --auth=none would be
// silently overridden; Set tells it from --auth left at the flag's
// default, which stays none for single.
func checkAuth(shapeName string, options newOpts) error {
	switch {
	case shapeName == "bare" && options.Auth != "none":
		return fmt.Errorf(newBareAuthMsg, options.Auth)
	case shapeName == "accounts" && options.Set["auth"] &&
		options.Auth == "none":
		return errors.New(newAccountsAuthNoneMsg)
	}
	return nil
}

// writeAccounts is single with sign-in under the menu's name, so the two
// cannot drift into separate sign-in scaffolds kept in step by hand.
func writeAccounts(options newOpts) error {
	if !options.Set["auth"] {
		options.Auth = "signin"
	}
	return writeSingle(options)
}

// designFlags only choose design-system files, so with it off each one
// typed would be silently ignored.
var designFlags = []string{"icons", "icon-delivery", "theme", "shell"}

// checkDesignSystem refuses --design-system=false where it cannot hold:
// single and accounts render their token, sign-in and error pages
// through it. Set, not the values: --theme=day typed out is a choice
// the app would never get.
func checkDesignSystem(shapeName string, options newOpts) error {
	if options.DesignSystem {
		return nil
	}
	if shapeName != "bare" {
		return errors.New(newDesignSystemShapeMsg)
	}
	for _, flagName := range designFlags {
		if options.Set[flagName] {
			return fmt.Errorf(newDesignFlagOffMsg, flagName)
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
	{Name: defaultShape, Summary: "one app and its SQLite data",
		Write: writeSingle},
	{Name: "accounts", Summary: "single with sign-in by emailed link",
		Write: writeAccounts},
	{Name: "home", Reserved: newReservedHomeMsg},
	{Name: "hub", Reserved: newReservedHubMsg},
}

// shapeNames is the shapes someone can scaffold today, in menu order.
func shapeNames() []string {
	var names []string
	for _, candidate := range shapes {
		if candidate.Reserved == "" {
			names = append(names, candidate.Name)
		}
	}
	return names
}

func lookupShape(name string) (shape, error) {
	for _, candidate := range shapes {
		if candidate.Name != name {
			continue
		}
		if candidate.Reserved != "" {
			return shape{}, fmt.Errorf(newReservedShapeMsg, name,
				candidate.Reserved)
		}
		return candidate, nil
	}
	return shape{}, fmt.Errorf(newUnknownShapeMsg, name)
}
