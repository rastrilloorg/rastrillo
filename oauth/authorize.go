package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const (
	codeLife  = time.Minute
	maxAnswer = 4 << 10
	// maxState bounds what each pending approval stores for the client.
	maxState = 2048
)

// What the error page says when the package itself refuses.
const (
	msgUnknownClient = "We couldn't identify the app asking to connect. " +
		"Go back to the app and try connecting again."
	msgBadRedirect = "The app tried to send you to an address it hasn't " +
		"approved, so it can't connect. Contact the app's maker."
	msgFailed = "Something went wrong on our side. " +
		"Try again in a moment."
	msgExpired = "This request has expired or was already answered. " +
		"Go back to the app and connect again."
	msgBadForm = "This approval form is incomplete. " +
		"Go back to the app and connect again."
	msgCrossSite = "We couldn't verify this request, so we didn't " +
		"accept it. Go back to the app and try connecting again."
	msgSignedOut = "You're signed out. Sign in, then go back to the app " +
		"and connect again."
	msgRefused    = "Your account can't connect apps to this site."
	msgBadRequest = "The app's request was missing something or asked " +
		"for something this site doesn't offer, so it can't connect."
	msgDenied = "You denied the request, so the app isn't connected."
)

// authorize answers GET /oauth/authorize: it checks the request, makes
// sure the right person is signed in, and shows the approve page.
func (server *Server) authorize(
	response http.ResponseWriter, request *http.Request,
) {
	req, ok := server.checked(response, request)
	if !ok {
		return
	}
	sess, ok := server.signedIn(response, request, request.URL.RequestURI())
	if !ok {
		return
	}
	pending := Pending{
		Subject: sess.Subject, ClientID: req.client.ID,
		ClientName: req.client.Name, Redirect: req.redirect,
		Challenge: req.challenge, Resource: req.resource,
		State: req.state, StateSent: req.stateSent,
		Return: request.URL.RequestURI(), Requested: req.access,
		Expires: server.cfg.Now().Add(pendingLife),
	}
	var err error
	if pending.ID, err = newID(); err == nil {
		err = server.cfg.Store.SavePending(request.Context(), pending)
	}
	if err != nil {
		server.cfg.Logger.Error("oauth: saving a pending approval", "err", err)
		server.linkBack(response, request, http.StatusInternalServerError,
			msgFailed, req.back, "server_error")
		return
	}
	page := Page{
		Kind: "approve", SiteName: server.cfg.SiteName,
		ClientName: req.client.Name, SelfNamed: !req.client.Document,
		ReturnHost: hostOf(req.redirect),
		Loopback:   loopback(hostOf(req.redirect)),
		Requested:  req.access, Pending: pending.ID,
	}
	if req.client.Document {
		page.ClientDomain = hostOf(req.client.ID)
	}
	server.render(response, request, http.StatusOK, page)
}

// request is an authorization request that passed every check.
type request struct {
	back
	client              Client
	challenge, resource string
	access              rastrillo.Access
}

// checked returns the request if it passes every check. Otherwise it
// has answered with the error page, which links back to the client
// once the redirect is trusted. Nothing redirects by itself before
// the person answers, because anyone can register a redirect
// (RFC 9700 §4.11.2).
func (server *Server) checked(
	response http.ResponseWriter, incoming *http.Request,
) (req request, ok bool) {
	query := incoming.URL.Query()
	id, ok := one(query, "client_id")
	client, err := server.clients.resolve(incoming.Context(), id)
	if !ok || err != nil {
		if err != nil && !errors.Is(err, ErrNotFound) && !documentID(id) {
			server.failed(response, incoming, "resolving client", err)
			return req, false
		}
		server.errorPage(response, incoming, http.StatusBadRequest,
			msgUnknownClient)
		return req, false
	}
	redirect, ok := one(query, "redirect_uri")
	if !ok || !redirectAllowed(client.Redirects, redirect) {
		server.errorPage(response, incoming, http.StatusBadRequest,
			msgBadRedirect)
		return req, false
	}
	state, ok := one(query, "state")
	to := back{redirect, state, ok && query.Has("state")}
	problem := "invalid_request"
	if ok && len(state) <= maxState {
		req, problem = server.params(query)
	}
	if problem != "" {
		server.linkBack(response, incoming, http.StatusBadRequest,
			msgBadRequest, to, problem)
		return req, false
	}
	req.client, req.back = client, to
	return req, true
}

// params checks the parameters that are reported to the client, in
// order, and returns the OAuth error code for the first one wrong.
func (server *Server) params(query url.Values) (request, string) {
	var req request
	rt, ok := one(query, "response_type")
	if !ok || rt != "code" {
		return req, "invalid_request"
	}
	method, ok1 := one(query, "code_challenge_method")
	ch, ok2 := one(query, "code_challenge")
	if !ok1 || !ok2 || method != "S256" || !validChallenge(ch) {
		return req, "invalid_request"
	}
	res, ok := one(query, "resource")
	if !ok || res != server.issuer+doorMCP && res != server.issuer+doorJSON {
		return req, "invalid_target"
	}
	scope, ok := one(query, "scope")
	access, valid := parseScope(scope)
	if !ok || !valid {
		return req, "invalid_scope"
	}
	return request{challenge: ch, resource: res, access: access}, ""
}

// parseScope maps a scope to the access it asks for: write if it names
// write, read if it names only read or nothing.
func parseScope(scope string) (rastrillo.Access, bool) {
	access := rastrillo.Read
	if scope == "" {
		return access, true
	}
	for _, word := range strings.Split(scope, " ") {
		switch word {
		case "read":
		case "write":
			access = rastrillo.Write
		default:
			return 0, false
		}
	}
	return access, true
}

// validChallenge follows RFC 7636 §4.2: 43 to 128 unreserved characters.
func validChallenge(ch string) bool {
	if len(ch) < 43 || len(ch) > 128 {
		return false
	}
	return strings.IndexFunc(ch, func(character rune) bool {
		return !('a' <= character && character <= 'z' ||
			'A' <= character && character <= 'Z' ||
			'0' <= character && character <= '9' ||
			strings.ContainsRune("-._~", character))
	}) < 0
}

// signedIn returns the session of a person who is signed in recently
// enough and allowed to connect. Otherwise it has answered: to sign
// in, or sign in again, and come back to returnTo, or the error page.
func (server *Server) signedIn(
	response http.ResponseWriter, request *http.Request, returnTo string,
) (sessions.Session, bool) {
	sess, ok := server.cfg.Session(request)
	if !ok {
		http.Redirect(response, request, server.cfg.SignIn(returnTo, false),
			http.StatusSeeOther)
		return sess, false
	}
	if !sessions.Fresh(sess, server.cfg.Fresh, server.cfg.Now()) {
		http.Redirect(response, request, server.cfg.SignIn(returnTo, true),
			http.StatusSeeOther)
		return sess, false
	}
	ok, refusal, err := server.cfg.Allowed(request.Context(), sess.Subject)
	if err != nil {
		server.failed(response, request, "checking the person is allowed", err)
		return sess, false
	}
	if !ok {
		if refusal == "" {
			refusal = msgRefused
		}
		server.errorPage(response, request, http.StatusForbidden, refusal)
		return sess, false
	}
	return sess, true
}

// answer takes POST /oauth/authorize, the person's answer on the
// approve page.
func (server *Server) answer(
	response http.ResponseWriter, request *http.Request,
) {
	ctx, now := request.Context(), server.cfg.Now()
	form, ok := server.answerForm(response, request)
	if !ok {
		return
	}
	// The approval is bound to a subject, so with nobody signed in it
	// cannot be read, nor its return address found.
	sess, ok := server.cfg.Session(request)
	if !ok {
		server.errorPage(response, request, http.StatusForbidden, msgSignedOut)
		return
	}
	pending, err := server.cfg.Store.Pending(
		ctx, form.pending, sess.Subject, now)
	if err != nil {
		server.notPending(response, request, err)
		return
	}
	if sess, ok = server.signedIn(response, request, pending.Return); !ok {
		return
	}
	pending, err = server.cfg.Store.TakePending(ctx, form.pending,
		sess.Subject, now)
	if err != nil {
		server.notPending(response, request, err)
		return
	}
	server.decide(response, request, pending, form, now)
}

// decide carries out the person's answer to a pending approval they
// have taken.
func (server *Server) decide(
	response http.ResponseWriter, request *http.Request, pending Pending,
	form answerForm, now time.Time,
) {
	to := back{pending.Redirect, pending.State, pending.StateSent}
	if !form.approve {
		// Anyone can register a redirect, so denying one is no choice
		// to visit it: only the page's link goes back (RFC 9700
		// §4.11.2).
		server.linkBack(response, request, http.StatusOK, msgDenied, to,
			"access_denied")
		return
	}
	access := min(pending.Requested, form.access)
	code, err := server.connect(request.Context(), pending, access, now)
	if err != nil {
		server.cfg.Logger.Error("oauth: creating a connection", "err", err)
		server.redirect(response, request, to, "error", "server_error")
		return
	}
	server.redirect(response, request, to, "code", code)
}

type answerForm struct {
	pending string
	approve bool
	access  rastrillo.Access
}

// answerForm reads the approve page's form from this site. Otherwise
// it has answered with the error page.
func (server *Server) answerForm(
	response http.ResponseWriter, request *http.Request,
) (form answerForm, ok bool) {
	if !csrf.SameOrigin(request, server.cfg.Origin) {
		server.errorPage(response, request, http.StatusForbidden, msgCrossSite)
		return form, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxAnswer)
	if err := request.ParseForm(); err != nil {
		server.errorPage(response, request, http.StatusBadRequest, msgBadForm)
		return form, false
	}
	decision := request.PostForm.Get("decision")
	access, err := parseAccess(request.PostForm.Get("access"))
	form = answerForm{
		pending: request.PostForm.Get("pending"),
		approve: decision == "approve", access: access,
	}
	if form.pending == "" || decision != "approve" && decision != "deny" ||
		form.approve && err != nil {
		server.errorPage(response, request, http.StatusBadRequest, msgBadForm)
		return form, false
	}
	return form, true
}

func (server *Server) notPending(
	response http.ResponseWriter, request *http.Request, err error,
) {
	if errors.Is(err, ErrNotFound) {
		server.errorPage(response, request, http.StatusBadRequest, msgExpired)
		return
	}
	server.failed(response, request, "reading a pending approval", err)
}

// connect stores the connection pending approved and its first code,
// and returns the code.
func (server *Server) connect(
	ctx context.Context, pending Pending, access rastrillo.Access,
	now time.Time,
) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(code))
	err = server.cfg.Store.CreateConnection(ctx, Connection{
		ID: id, Subject: pending.Subject, ClientID: pending.ClientID,
		ClientName: pending.ClientName, ReturnHost: hostOf(pending.Redirect),
		Resource: pending.Resource, Access: access, Created: now, LastUsed: now,
	}, Code{
		Hash: hash[:], Connection: id, ClientID: pending.ClientID,
		Redirect: pending.Redirect, Challenge: pending.Challenge,
		Resource: pending.Resource,
		Expires:  now.Add(codeLife),
	})
	return code, err
}

func (server *Server) failed(
	response http.ResponseWriter, request *http.Request, doing string,
	err error,
) {
	server.cfg.Logger.Error("oauth: "+doing, "err", err)
	server.errorPage(response, request, http.StatusInternalServerError,
		msgFailed)
}

// one returns the parameter's value; ok is false if it is repeated,
// since RFC 6749 §3.1 refuses a request that sends one twice.
func one(query url.Values, name string) (string, bool) {
	values := query[name]
	if len(values) > 1 {
		return "", false
	}
	if len(values) == 0 {
		return "", true
	}
	return values[0], true
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
