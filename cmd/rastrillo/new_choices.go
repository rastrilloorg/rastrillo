package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/iconsets"
	"amadan.net/rastrillo/rastrillo/ui"
)

// newChoices are the icon, UX, theme, shell and locale flags resolved
// to what a scaffold writes.
type newChoices struct {
	rendered    iconsets.Rendered
	conventions []byte
	vendored    map[string][]byte
	layout      []byte
	code        string
}

// checkChoices refuses a bad icon, delivery, profile, theme, shell or
// locale. runNew calls it for every shape before the shape's Write: an
// unknown choice must fail while the working directory is still clean,
// never halfway through a scaffold, and no shape may forget to.
func checkChoices(options newOpts) error {
	_, err := resolveChoices(options)
	return err
}

func resolveChoices(options newOpts) (newChoices, error) {
	rendered, err := iconsets.Render(options.IconSet,
		iconsets.Delivery(options.IconDelivery))
	if err != nil {
		return newChoices{}, err
	}
	// The conventions record the RESOLVED choices, not the profile's
	// defaults: the flag wins, and the file says what actually happened.
	conventions, err := conventionsSection(options.UX, options.IconSet,
		options.IconDelivery)
	if err != nil {
		return newChoices{}, err
	}
	// The vendored set — the stylesheets, the chosen theme and the
	// scripts — comes from ui.VendoredAssets, which is the one place
	// that list is written down (ui/vendored.go). The generated
	// vendored_test.go single writes and `rastrillo doctor` read the same
	// function, so a file added to the set reaches the scaffold, the
	// pin and the doctor together rather than one at a time.
	vendored, ok := ui.VendoredAssets(options.Theme)
	if !ok {
		return newChoices{}, fmt.Errorf("unknown theme %q: known themes "+
			"are %s", options.Theme, strings.Join(ui.ThemeNames(), ", "))
	}
	layoutHTML, ok := ui.Layout(options.Shell)
	if !ok {
		return newChoices{}, fmt.Errorf("unknown shell %q: known shells "+
			"are %s", options.Shell, strings.Join(ui.LayoutNames(), ", "))
	}
	code, err := newLocale(options.Locale)
	if err != nil {
		return newChoices{}, err
	}
	return newChoices{rendered, conventions, vendored, layoutHTML, code},
		nil
}

// newLocale is --locale as the code the app starts with. Only a language
// whose framework strings ship: any other fails generate --check on its
// missing catalog before the author writes a line.
func newLocale(locale string) (string, error) {
	code := cmp.Or(locale, "en")
	// A code from before a retag: pt means pt-PT.
	if to, ok := rastrillo.RetagOf(code); ok {
		code = to
	}
	if !slices.Contains(rastrillo.BaseLocales(), code) {
		return "", fmt.Errorf(newLocaleUnsupportedMsg, code,
			strings.Join(rastrillo.BaseLocales(), ", "))
	}
	return code, nil
}
