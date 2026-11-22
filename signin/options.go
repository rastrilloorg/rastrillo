package signin

import (
	"errors"
	"net/http"

	"amadan.net/rastrillo/rastrillo/passkey"
)

// Options is the Sign-in options page: the address the email link goes
// to, which is always on, and a row for each way the app turns on.
type Options struct {
	Address string // "" when signin cannot tell it
	Ways    []OptionRow
	// RemovePath is where a row's Remove form posts way, and for a
	// passkey its id.
	RemovePath string
	// Removed says a way was just taken off.
	Removed bool
}

// OptionRow is one way: a way the person has, with a Remove form, or an
// enabled way they lack, with a link to its add page. A passkey has a
// row for each credential, named by Label, which may be empty.
type OptionRow struct {
	Way     Ways
	Have    bool
	AddPath string // where to add it; "" once it is had
	Label   string
	ID      string // a passkey's credential
}

// answered records, once a way is stored, that subject added one, which
// answers the offer for good: removing the last way later must not
// bring it back, whether or not they ever chose "Not now". Only a stored
// way answers it, so a slip and a closed tab never lose the offer. When
// the record fails, undo takes the way back out, so a way is never kept
// without its answer; it answers the 500 itself and reports whether to
// go on.
func (signins *Signins) answered(
	response http.ResponseWriter, request *http.Request, subject string,
	undo func() error,
) bool {
	err := signins.dismiss(request.Context(), subject)
	if err == nil {
		return true
	}
	if undone := undo(); undone != nil {
		signins.config.Logger.Error("rastrillo/signin: undo an added way",
			"err", undone)
	}
	signins.broke(response, "record the answered offer", err)
	return false
}

// ShowOptions is GET OptionsPath. Looking needs only a session;
// changing a way needs a strong one.
func (signins *Signins) ShowOptions(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.signedIn(response, request)
	if !ok {
		return
	}
	rows, err := signins.optionRows(request, session.Subject)
	if err != nil {
		signins.broke(response, "list the sign-in options", err)
		return
	}
	signins.config.Render(response, request, View{
		Status: http.StatusOK,
		Options: &Options{
			Address:    signins.address(request, session.Subject),
			Ways:       rows,
			RemovePath: OptionsRemovePath,
			Removed:    request.URL.Query().Get("removed") == "1",
		},
	})
}

// optionRows is one row for each way that is on, passkeys first, so the
// page reads in the order the offer recommends them.
func (signins *Signins) optionRows(
	request *http.Request, subject string,
) ([]OptionRow, error) {
	var rows []OptionRow
	ways := signins.config.Ways
	if ways&Passkey != 0 {
		list, err := signins.passkeys.List(subject)
		if err != nil {
			return nil, err
		}
		for _, info := range list {
			rows = append(rows, OptionRow{Way: Passkey, Have: true,
				Label: info.Label, ID: info.ID})
		}
		if len(list) == 0 {
			rows = append(rows, OptionRow{Way: Passkey,
				AddPath: AddPasskeyPath + carry(OptionsPath)})
		}
	}
	if ways&Authenticator != 0 {
		has, err := signins.codes.Enrolled(subject)
		if err != nil {
			return nil, err
		}
		rows = append(rows, signins.secretRow(Authenticator, has))
	}
	if ways&Passphrase != 0 {
		_, has, err := signins.passphrases.hash(request.Context(), subject)
		if err != nil {
			return nil, err
		}
		rows = append(rows, signins.secretRow(Passphrase, has))
	}
	return rows, nil
}

// secretRow is a way that has one row, with or without its add link.
// Adding returns to the options page, where the new way shows.
func (signins *Signins) secretRow(way Ways, has bool) OptionRow {
	row := OptionRow{Way: way, Have: has}
	if !has {
		row.AddPath = way.addPath() + carry(OptionsPath)
	}
	return row
}

// RemoveWay is POST OptionsRemovePath: way is passkey, authenticator or
// passphrase, and for a passkey id says which. A way that is off or not
// the person's is a 404, a session that is no longer strong goes to
// sign in again and comes back to the options page.
func (signins *Signins) RemoveWay(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.session(request)
	if !ok {
		signins.toOptions(response, request, false, false)
		return
	}
	way, known := wayNamed(request.PostFormValue("way"))
	if !known {
		http.Error(response, http.StatusText(http.StatusBadRequest),
			http.StatusBadRequest)
		return
	}
	if signins.config.Ways&way == 0 {
		http.NotFound(response, request)
		return
	}
	if !signins.strong(session) {
		signins.toOptions(response, request, true, mustReauth(session))
		return
	}
	err := signins.take(request, way, session.Subject)
	switch {
	case errors.Is(err, passkey.ErrNotYours):
		http.NotFound(response, request)
	case err != nil:
		signins.broke(response, "remove a sign-in way", err)
	default:
		http.Redirect(response, request, OptionsPath+"?removed=1",
			http.StatusSeeOther)
	}
}

// toOptions sends the browser to sign in and come back to the options
// page, which a POST's own address could not be returned to.
func (signins *Signins) toOptions(
	response http.ResponseWriter, request *http.Request, reauth, force bool,
) {
	http.Redirect(response, request,
		signins.signinURL(OptionsPath, reauth, force), http.StatusSeeOther)
}

// take removes way through the store that owns it: passkey and totp
// check ownership and secrets themselves, signin keeps the passphrase.
func (signins *Signins) take(
	request *http.Request, way Ways, subject string,
) error {
	switch way {
	case Passkey:
		return signins.passkeys.Remove(subject, request.PostFormValue("id"))
	case Authenticator:
		return signins.codes.Disable(subject)
	}
	return signins.passphrases.remove(request.Context(), subject)
}

// wayNamed maps the form field back to a Ways bit, and refuses anything
// else so a crafted POST never reaches a store.
func wayNamed(name string) (Ways, bool) {
	for _, way := range []Ways{Passkey, Authenticator, Passphrase} {
		if way.Name() == name {
			return way, true
		}
	}
	return 0, false
}
