package signin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// methodLink is the session method of an emailed link, the one sign-in
// that leaves a person with no faster way back.
const methodLink = "magiclink"

// offerDue is whether to show the offer of a faster way in: the session
// came from an emailed link, the app has a way to add, the person has
// none of the ways on, and they have not chosen "Not now". Keymail and
// every added way already have something faster than a link.
func (signins *Signins) offerDue(
	ctx context.Context, session sessions.Session,
) (bool, error) {
	if session.Method != methodLink || signins.config.Ways == 0 {
		return false, nil
	}
	// Not now first: it is one row, and the gate asks on every page a
	// link user opens long after they answered.
	dismissed, err := signins.dismissed(ctx, session.Subject)
	if err != nil || dismissed {
		return false, err
	}
	has, err := signins.hasAWay(ctx, session.Subject)
	return !has && err == nil, err
}

// hasAWay is whether subject already has one of the ways that are on.
// A way that is off does not count: the person cannot use it here.
func (signins *Signins) hasAWay(
	ctx context.Context, subject string,
) (bool, error) {
	ways := signins.config.Ways
	if ways&Passkey != 0 {
		if has, err := signins.passkeys.Enrolled(subject); err != nil ||
			has {
			return has, err
		}
	}
	if ways&Authenticator != 0 {
		if has, err := signins.codes.Enrolled(subject); err != nil || has {
			return has, err
		}
	}
	if ways&Passphrase != 0 {
		_, has, err := signins.passphrases.hash(ctx, subject)
		return has, err
	}
	return false, nil
}

func (signins *Signins) dismissed(
	ctx context.Context, subject string,
) (bool, error) {
	var one int
	err := signins.config.DB.QueryRowContext(ctx,
		`SELECT 1 FROM signin_offers WHERE subject = ?`, subject).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// dismiss remembers that subject answered the offer. The first answer
// stands, so a second tab's "Not now" changes nothing.
func (signins *Signins) dismiss(ctx context.Context, subject string) error {
	return signins.dismissIn(ctx, signins.config.DB, subject)
}

func (signins *Signins) dismissIn(
	ctx context.Context, database execer, subject string,
) error {
	_, err := database.ExecContext(ctx,
		`INSERT INTO signin_offers (subject, dismissed_at) VALUES (?, ?)
		 ON CONFLICT (subject) DO NOTHING`,
		subject, signins.config.Now().UTC().Format(time.RFC3339))
	return err
}

// ShowOffer is GET OfferPath, where the gate sends a person while the
// offer is due. Once it is not, the page goes on to return_to, so a
// bookmark or a second tab never shows it again.
func (signins *Signins) ShowOffer(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.signedIn(response, request)
	if !ok {
		return
	}
	to := returnTo(request)
	due, err := signins.offerDue(request.Context(), session)
	switch {
	case err != nil:
		signins.broke(response, "the offer", err)
	case !due:
		http.Redirect(response, request, orHome(to), http.StatusSeeOther)
	default:
		signins.config.Render(response, request, View{
			Status: http.StatusOK,
			Offer: &Offer{Ways: signins.config.Ways,
				LaterPath: OfferLaterPath, ReturnTo: to},
		})
	}
}

// LaterOffer is POST OfferLaterPath, "Not now": remembered for good,
// then on to where the person was going.
func (signins *Signins) LaterOffer(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.signedIn(response, request)
	if !ok {
		return
	}
	if err := signins.dismiss(request.Context(), session.Subject); err != nil {
		signins.broke(response, "remember Not now", err)
		return
	}
	http.Redirect(response, request, orHome(returnTo(request)),
		http.StatusSeeOther)
}

// returnTo is the request's return_to when it is same-site, else "".
func returnTo(request *http.Request) string {
	return sessions.SafeReturnTo(request.FormValue("return_to"), "")
}

func orHome(to string) string {
	if to == "" {
		return "/"
	}
	return to
}

// signedIn is the request's session, or a 303 to the sign-in page that
// comes back here.
func (signins *Signins) signedIn(
	response http.ResponseWriter, request *http.Request,
) (sessions.Session, bool) {
	session, ok := signins.session(request)
	if !ok {
		signins.toSignin(response, request, false, false)
	}
	return session, ok
}

func (signins *Signins) session(
	request *http.Request,
) (sessions.Session, bool) {
	return signins.config.Sessions.From(request)
}

// broke answers an error the person cannot fix with a plain 500, and
// logs what was being done.
func (signins *Signins) broke(
	response http.ResponseWriter, what string, err error,
) {
	signins.config.Logger.Error("rastrillo/signin: "+what, "err", err)
	http.Error(response, "Something went wrong. Please try again.",
		http.StatusInternalServerError)
}
