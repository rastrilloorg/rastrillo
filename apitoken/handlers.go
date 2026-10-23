package apitoken

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// HandlersConfig wires the token page. Store, Path and Render are
// required. Render draws the page from PageData, through ui's
// api-tokens partial or a template of the app's own.
type HandlersConfig struct {
	Store *Store
	// Path is the page's own path: where every revoke sends the
	// browser back to.
	Path string
	// Render draws the whole page around d. Cache-Control is already
	// set, and so is the status when it is not 200.
	Render func(w http.ResponseWriter, r *http.Request, d PageData)
	// Name turns a subject into the name the person recognises, for
	// "Claude for Paul". Nil, or "", shows the subject.
	Name func(ctx context.Context, subject string) string
}

// PageData is what Render receives: the caller's tokens, the create
// form, and the one-time reveal.
type PageData struct {
	// Tokens is every token the caller owns, newest first, revoked and
	// expired ones included.
	Tokens []Row
	// Agents is each agent name that still has a live token, once, in
	// Tokens' order: one "revoke all for this agent" button per agent,
	// not one per row. Worked out here because a template cannot
	// dedupe a list.
	Agents []string
	// Created is set only in the response to a successful Create. It
	// holds the only copy of the secret there will ever be.
	Created *Created
	// Errors is keyed by field: "agent" when the create form named no
	// agent and did not tick "for me". The values are translated.
	Errors map[string]string
	// Form echoes the create form's fields: agent, for_self ("on" or
	// ""), access ("write" or "read") and expiry ("30", "90", "365" or
	// "never"). A fresh form carries the defaults, write and 90.
	Form map[string]string
	// CSRFOK is always true: the app's csrf.Protect runs upstream, so
	// a template can render the form unconditionally.
	CSRFOK bool
}

// Row is one token on the page: the record, the words that say whose
// it is ("Claude for Paul", or the person alone), and its state against
// the store's clock, because a template has no clock of its own. A row
// that is neither revoked nor expired is live and gets a revoke button.
type Row struct {
	Token
	Display string
	// Revoked is set when the token was revoked; Expired when its
	// expiry has passed. Both can be set on one row.
	Revoked, Expired bool
}

// Created is the one-time reveal: the whole token, and its record.
type Created struct {
	Secret string
	Token  Row
}

// Handlers are the token page's doors. Mount them under the session
// router, behind csrf.Protect, with Create also behind
// sessions.RequireFresh(10 * time.Minute).
type Handlers struct{ cfg HandlersConfig }

// NewHandlers validates cfg.
func NewHandlers(cfg HandlersConfig) (*Handlers, error) {
	if cfg.Store == nil {
		return nil, errors.New("rastrillo/apitoken: HandlersConfig.Store is required")
	}
	if cfg.Path == "" {
		return nil, errors.New("rastrillo/apitoken: HandlersConfig.Path is required; revokes redirect there")
	}
	if cfg.Render == nil {
		return nil, errors.New("rastrillo/apitoken: HandlersConfig.Render is required")
	}
	return &Handlers{cfg: cfg}, nil
}

// Form field names and the values the page's selects carry.
const (
	fieldAgent   = "agent"
	fieldForSelf = "for_self"
	fieldAccess  = "access"
	fieldExpiry  = "expiry"
	fieldID      = "id"
)

// accesses and expiries are the radio's and the select's values: the
// form's own contract, kept here rather than borrowed from the store's
// column parsing so the page and the table can change independently.
var accesses = map[string]rastrillo.Access{"read": rastrillo.Read, "write": rastrillo.Write}

// expiries maps the expiry select's values to a TTL; "never" is 0.
var expiries = map[string]time.Duration{
	"30":    30 * 24 * time.Hour,
	"90":    90 * 24 * time.Hour,
	"365":   365 * 24 * time.Hour,
	"never": 0,
}

func defaultForm() map[string]string {
	return map[string]string{fieldAgent: "", fieldForSelf: "", fieldAccess: "write", fieldExpiry: "90"}
}

// begin is the start of every handler: no-store, because each
// response is either the reveal or a list of credentials, and a
// cached copy of either is a credential page in a shared browser; then
// the subject, from the session alone. A form field cannot name whose
// tokens are read or revoked, so a forged one is never looked at.
func (h *Handlers) begin(w http.ResponseWriter, r *http.Request, method string) (string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != method {
		// Refuse before the session check, so a GET to Create cannot
		// mint from its query string: csrf.Protect guards POST alone,
		// and a link is all it would take.
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return "", false
	}
	sess, ok := sessions.Current(r)
	if !ok || sess.Subject == "" {
		http.Error(w, "signed out", http.StatusForbidden)
		return "", false
	}
	return sess.Subject, true
}

// Page is GET: the caller's tokens and an empty create form.
func (h *Handlers) Page(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.begin(w, r, http.MethodGet)
	if !ok {
		return
	}
	h.show(w, r, subject, http.StatusOK, PageData{Form: defaultForm()})
}

// Create is POST: it mints a token and renders the page with Created
// set, in this response and nowhere else. No redirect, no cookie, no
// flash, nothing in a URL: each of those is a second copy of the
// secret, in a place the person did not choose. Reloading re-submits
// the form and mints a second token, which the browser warns about.
//
// Mount it behind sessions.RequireFresh(10 * time.Minute): a token
// outlives the session that made it, so minting one is the moment to
// be sure the person at the keyboard is the account's owner. The
// handler does not check freshness itself, because the gate works
// whichever sign-in plugin the app uses and the handler cannot see
// which one that is.
func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// PostForm, not Form: the query string is not the form, and a
	// value smuggled there must not reach Mint.
	form := map[string]string{
		fieldAgent:   r.PostForm.Get(fieldAgent),
		fieldForSelf: r.PostForm.Get(fieldForSelf),
		fieldAccess:  r.PostForm.Get(fieldAccess),
		fieldExpiry:  r.PostForm.Get(fieldExpiry),
	}
	// Access and expiry come from a radio and a select. A value the
	// page never offers is a tampered form, not a slip, so it gets a
	// 400 rather than a default: minting a write token in place of a
	// mangled "read" is the wrong way to be forgiving.
	access, ok := accesses[form[fieldAccess]]
	if !ok {
		http.Error(w, "access must be read or write", http.StatusBadRequest)
		return
	}
	ttl, ok := expiries[form[fieldExpiry]]
	if !ok {
		http.Error(w, "expiry must be 30, 90, 365 or never", http.StatusBadRequest)
		return
	}
	in := MintInput{Access: access, TTL: ttl, ForSelf: form[fieldForSelf] != ""}
	if !in.ForSelf {
		agent, ok := ValidAgentName(form[fieldAgent])
		if !ok {
			h.show(w, r, subject, http.StatusUnprocessableEntity, PageData{
				Errors: map[string]string{fieldAgent: translator(r)("rastrillo.ui.api_tokens_error_agent")},
				Form:   form,
			})
			return
		}
		in.Agent = agent
	}
	secret, tok, err := h.cfg.Store.Mint(r.Context(), subject, in)
	if err != nil {
		http.Error(w, "could not create the token", http.StatusInternalServerError)
		return
	}
	h.show(w, r, subject, http.StatusOK, PageData{
		Created: &Created{Secret: secret, Token: h.row(r, tok)},
		Form:    defaultForm(),
	})
}

// Revoke is POST with the token's public ID in "id". Another person's
// ID, or an unknown one, is a 404: the store's query binds the id to
// the subject, and this handler does not say which of the two failed.
func (h *Handlers) Revoke(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	err := h.cfg.Store.Revoke(r.Context(), subject, r.PostForm.Get(fieldID))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not revoke the token", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, h.cfg.Path, http.StatusSeeOther)
}

// RevokeAgent is POST with the agent's name in "agent": every live
// token the caller made for that agent ends. A name the page could
// not have offered is a 400; an agent with no live tokens is simply
// a redirect, because a kill switch that is already thrown is not an
// error.
func (h *Handlers) RevokeAgent(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	agent, ok := ValidAgentName(r.PostForm.Get(fieldAgent))
	if !ok {
		http.Error(w, "agent must be a name", http.StatusBadRequest)
		return
	}
	if _, err := h.cfg.Store.RevokeAgent(r.Context(), subject, agent); err != nil {
		http.Error(w, "could not revoke the tokens", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, h.cfg.Path, http.StatusSeeOther)
}

// RevokeAll is POST: every live token the caller owns ends.
func (h *Handlers) RevokeAll(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.begin(w, r, http.MethodPost)
	if !ok {
		return
	}
	if _, err := h.cfg.Store.RevokeAll(r.Context(), subject); err != nil {
		http.Error(w, "could not revoke the tokens", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, h.cfg.Path, http.StatusSeeOther)
}

// show fills in the list and renders. The list is read on every
// render, the reveal included, so the new token is in it.
func (h *Handlers) show(w http.ResponseWriter, r *http.Request, subject string, status int, d PageData) {
	tokens, err := h.cfg.Store.List(r.Context(), subject)
	switch {
	case err != nil && d.Created != nil:
		// Mint has already inserted the row. A 500 here would leave a
		// live token whose secret nobody saw and nobody can recover,
		// which is worse than a page with a short list: the reveal is
		// the only copy, so it is shown with the one row known to be
		// there. Listing before minting would not do, because the
		// list is read again after Mint to show the new token in it.
		d.Tokens = []Row{d.Created.Token}
	case err != nil:
		http.Error(w, "could not list the tokens", http.StatusInternalServerError)
		return
	default:
		d.Tokens = make([]Row, 0, len(tokens))
		for _, t := range tokens {
			d.Tokens = append(d.Tokens, h.row(r, t))
		}
	}
	now := h.cfg.Store.now()
	d.Agents = settle(d.Tokens, now)
	if d.Created != nil {
		// The reveal holds its own copy of the row, outside the list.
		one := []Row{d.Created.Token}
		settle(one, now)
		d.Created.Token = one[0]
	}
	d.CSRFOK = true
	// Only a non-200 status is written here: once WriteHeader has run,
	// a header Render sets (its Content-Type, say) is dropped, so the
	// common path leaves the implicit 200 to the renderer, as
	// password's show does.
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	h.cfg.Render(w, r, d)
}

// row names the token's actor in the page's language, in Go rather
// than in a template, because Name may need the request's context.
func (h *Handlers) row(r *http.Request, t Token) Row {
	var name func(string) string
	if h.cfg.Name != nil {
		name = func(subject string) string { return h.cfg.Name(r.Context(), subject) }
	}
	return Row{Token: t, Display: rastrillo.ActorPhrase(t.Actor(), name, translator(r))}
}

// settle marks each row's state at now and returns the agents that
// still have a live token, once each, in the rows' order. show runs
// the list and the reveal's own row through it, so the two copies of
// a just-minted token say the same thing.
func settle(rows []Row, now time.Time) []string {
	var agents []string
	live := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		row.Revoked = row.RevokedAt != nil
		row.Expired = row.ExpiresAt != nil && !now.Before(*row.ExpiresAt)
		if row.Agent != "" && !row.Revoked && !row.Expired && !live[row.Agent] {
			live[row.Agent] = true
			agents = append(agents, row.Agent)
		}
	}
	return agents
}

// translator is the request's translation function. Outside the
// Locales middleware rastrillo.Tf answers with the key itself, which
// would put "rastrillo.ui.agent_for" on the page; the base catalog's
// English is the right fallback there, with the same {name} filling.
func translator(r *http.Request) func(key string, args ...any) string {
	if rastrillo.LocaleFrom(r) != "" {
		return func(key string, args ...any) string { return rastrillo.Tf(r, key, args...) }
	}
	return func(key string, args ...any) string {
		s, ok := rastrillo.BaseCatalog()[key]
		if !ok {
			return key
		}
		for i := 0; i+1 < len(args); i += 2 {
			s = strings.ReplaceAll(s, "{"+fmt.Sprint(args[i])+"}", fmt.Sprint(args[i+1]))
		}
		return s
	}
}
