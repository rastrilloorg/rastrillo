package handoff

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// maxFormBytes is far above an assertion's 8 KiB ceiling and far below
// what would let a stranger make the server buffer much.
const maxFormBytes = 16 << 10

// These carry no submitted values, like assertion's, so a refusal can be
// logged as it is.
var (
	errForm      = errors.New("handoff: unreadable form")
	errNoCookie  = errors.New("handoff: no nonce cookie")
	errNonce     = errors.New("handoff: nonce mismatch")
	errSuspended = errors.New("handoff: account suspended")
	errReplay    = errors.New("handoff: replayed assertion")
	errInternal  = errors.New("handoff: internal")
	errNoSubject = errors.New("handoff: SubjectFor returned an empty subject")
)

// Consume finishes a handoff: it checks the root's assertion against the
// nonce Enter bound to this browser, spends it, and signs the person in.
func (handoff *Handoff) Consume(
	response http.ResponseWriter, request *http.Request,
) {
	// The same reason as Enter's: path tenants use the hub's session.
	if basepath.PathRouted(request) {
		http.NotFound(response, request)
		return
	}
	// A handoff belongs to one origin; a session signed in on another
	// host would be one the root never vouched for there.
	if !basepath.SameHost(request.Host, handoff.origin) {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed),
			http.StatusMethodNotAllowed)
		return
	}
	claims, next, err := handoff.admit(response, request)
	if err != nil {
		handoff.refuse(response, request, err)
		return
	}
	err = handoff.sessions.SignIn(response, request, sessions.Session{
		Subject:  claims.Subject,
		Method:   Method,
		AuthTime: time.Unix(claims.AuthTime, 0),
	})
	if err != nil {
		// The assertion stays spent: it is single-use whatever happens.
		handoff.refuse(response, request, fmt.Errorf("%w: sign in: %w",
			errInternal, err))
		return
	}
	http.Redirect(response, request, next, http.StatusSeeOther)
}

// admit runs every check before spending the assertion's ID, so a
// forged or mistimed POST cannot burn a handoff someone is still on.
func (handoff *Handoff) admit(
	response http.ResponseWriter, request *http.Request,
) (assertion.Claims, string, error) {
	request.Body = http.MaxBytesReader(response, request.Body, maxFormBytes)
	// ParseForm's errors can quote the body, so none of them is kept.
	if request.ParseForm() != nil {
		return assertion.Claims{}, "", errForm
	}
	claims, err := handoff.verifier.Verify(
		request.PostForm.Get("assertion"), handoff.now())
	if err != nil {
		return assertion.Claims{}, "", err
	}
	next, err := handoff.matchNonce(request, claims.Nonce)
	if err != nil {
		return assertion.Claims{}, "", err
	}
	if err := handoff.checkSuspended(request.Context()); err != nil {
		clearNonceCookie(response)
		return assertion.Claims{}, "", err
	}
	// A refusal here leaves the cookie in place and does not spend the
	// assertion, so the browser can send the same POST again if
	// SubjectFor succeeds.
	claims.Subject, err = handoff.sessionSubject(request.Context(), claims)
	if err != nil {
		return assertion.Claims{}, "", err
	}
	clearNonceCookie(response)
	if err := handoff.spend(request.Context(), claims); err != nil {
		return assertion.Claims{}, "", err
	}
	return claims, next, nil
}

// matchNonce compares the assertion's nonce with this browser's and
// returns the page to land on. A mismatch leaves the cookie alone:
// someone else's assertion must not cancel this browser's own handoff.
func (handoff *Handoff) matchNonce(
	request *http.Request, signed string,
) (string, error) {
	cookie, err := request.Cookie(nonceCookie)
	if err != nil {
		return "", errNoCookie
	}
	nonce, next, ok := decodeNonceCookie(cookie.Value)
	if !ok || subtle.ConstantTimeCompare([]byte(nonce), []byte(signed)) != 1 {
		return "", errNonce
	}
	return next, nil
}

// sessionSubject returns the subject for the session: the root's
// subject, or the value returned by the app's SubjectFor.
func (handoff *Handoff) sessionSubject(
	ctx context.Context, claims assertion.Claims,
) (string, error) {
	if handoff.subjectFor == nil {
		return claims.Subject, nil
	}
	if err := handoff.checkUnspent(ctx, claims); err != nil {
		return "", err
	}
	subject, err := handoff.subjectFor(ctx, claims.Subject)
	if err != nil {
		return "", fmt.Errorf("handoff: SubjectFor: %w", err)
	}
	if subject == "" {
		return "", errNoSubject
	}
	return subject, nil
}

func (handoff *Handoff) checkSuspended(ctx context.Context) error {
	if handoff.suspended == nil {
		return nil
	}
	suspended, err := handoff.suspended(ctx)
	if err != nil {
		return fmt.Errorf("%w: suspended: %w", errInternal, err)
	}
	if suspended {
		return errSuspended
	}
	return nil
}

// checkUnspent refuses an assertion already spent, so a replay never
// runs the app's SubjectFor again. It is only a read: two first uses at
// once both pass it, and spend's insert is what refuses the second.
func (handoff *Handoff) checkUnspent(
	ctx context.Context, claims assertion.Claims,
) error {
	var spent bool
	err := handoff.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM handoff_used WHERE jti = ?)`,
		claims.ID).Scan(&spent)
	if err != nil {
		return fmt.Errorf("%w: check spent: %w", errInternal, err)
	}
	if spent {
		return errReplay
	}
	return nil
}

// spend records the assertion's ID. A second insert of the same ID
// changes no row, which is how a replay shows itself.
func (handoff *Handoff) spend(
	ctx context.Context, claims assertion.Claims,
) error {
	result, err := handoff.db.ExecContext(ctx,
		`INSERT INTO handoff_used (jti, expires_at) VALUES (?, ?)
		ON CONFLICT DO NOTHING`, claims.ID, claims.ExpiresAt)
	if err != nil {
		return fmt.Errorf("%w: spend: %w", errInternal, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: spend: %w", errInternal, err)
	}
	if inserted == 0 {
		return errReplay
	}
	return nil
}

func clearNonceCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name:     nonceCookie,
		Path:     "/",
		SameSite: http.SameSiteNoneMode,
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
	})
}

// refuse answers a failed handoff: a fault of ours is a 500, and anything
// else is a refusal page the person can act on.
func (handoff *Handoff) refuse(
	response http.ResponseWriter, request *http.Request, err error,
) {
	if errors.Is(err, errInternal) {
		handoff.logger.Error("handoff: consume", "err", err)
		http.Error(response,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}
	handoff.logger.Warn("handoff: refused", "err", err)
	page := RefusalPage{
		Title:    "We couldn't verify that link",
		Message:  "Go back and open your account again.",
		LinkText: "Go to the main site",
		LinkURL:  handoff.root,
	}
	// No link: the root would only send them straight back here.
	if errors.Is(err, errSuspended) {
		page = RefusalPage{
			Title: "This account is suspended",
			Message: "No one can sign in to this account right now. " +
				"If this is your account, contact the person who runs " +
				"the service.",
		}
	}
	response.Header().Set("Cache-Control", "no-store")
	handoff.renderRefusal(response, request, page)
}

var refusalTemplate = template.Must(template.New("refusal").Parse(
	`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
{{with .LinkURL}}<p><a href="{{.}}">{{$.LinkText}}</a></p>{{end}}
</main>
</body>
</html>
`))

func renderDefaultRefusal(
	response http.ResponseWriter, _ *http.Request, page RefusalPage,
) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusForbidden)
	// The status is already sent, so a failed write has no one to tell.
	_ = refusalTemplate.Execute(response, page)
}
