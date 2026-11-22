package signin

import (
	"html/template"
	"net/url"

	"amadan.net/rastrillo/rastrillo/auth"
)

// View is everything a page signin draws needs, handed to
// Config.Render. The app owns the layout, so signin hands it data and
// a status and never writes HTML itself. One of Signin, Offer, Add and
// Options is set, and ui's signin-view partial draws whichever it is.
type View struct {
	Status  int
	Signin  *auth.SigninState // the sign-in screen
	Offer   *Offer
	Add     *Add
	Options *Options
}

// Offer is "Sign in faster next time": the ways on, each a link to its
// add page, and "Not now", a form posting to LaterPath.
type Offer struct {
	Ways                Ways
	LaterPath, ReturnTo string
}

// Add is one way's add page, or, with Done, the line that says it
// worked and a link on to ReturnTo.
type Add struct {
	Way      Ways
	Action   string // where the form posts
	ReturnTo string
	QR       template.HTML // authenticator only
	Key      string        // authenticator only
	Problem  string        // a locale key, "" for none
	Done     bool
	Passkey  *auth.PasskeyDoor // passkey only: begin/finish and modules
	// Address names the account to a password manager and to the
	// passkey the device keeps; "" when signin cannot tell it.
	Address string
	// RevealURL is the passphrase's show/hide module; passphrase only.
	RevealURL string
}

// Choice is one row of the offer: a way's name and its add page.
type Choice struct {
	Way  string
	Href string
}

// Choices are the offer's rows, passkey first because it is the one
// to recommend, each carrying ReturnTo on to its add page.
func (offer Offer) Choices() []Choice {
	var choices []Choice
	for _, way := range []Ways{Passkey, Authenticator, Passphrase} {
		if offer.Ways&way != 0 {
			choices = append(choices, Choice{Way: way.Name(),
				Href: way.addPath() + carry(offer.ReturnTo)})
		}
	}
	return choices
}

// Name is a single way's word in templates and the screen:
// "passkey", "authenticator" or "passphrase"; "" for a set of several.
func (ways Ways) Name() string {
	switch ways {
	case Passkey:
		return "passkey"
	case Authenticator:
		return "authenticator"
	case Passphrase:
		return "passphrase"
	}
	return ""
}

func (ways Ways) addPath() string {
	switch ways {
	case Passkey:
		return AddPasskeyPath
	case Authenticator:
		return AddAuthenticatorPath
	}
	return AddPassphrasePath
}

// carry is the query that takes returnTo on to the next page, "" when
// there is nowhere to go back to.
func carry(returnTo string) string {
	if returnTo == "" {
		return ""
	}
	return "?return_to=" + url.QueryEscape(returnTo)
}
