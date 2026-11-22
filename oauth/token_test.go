package oauth_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/oauth"
)

const (
	mcpResource = "https://a.test/api/v1/mcp"
	day         = 24 * time.Hour
)

// tokenFlow is a flow whose server mints real tokens on the flow's
// clock, so expiry and ended connections show up in Verify.
type tokenFlow struct {
	*flow
	tokens  *apitoken.Store
	tokenDB *sql.DB
}

func newTokenFlow(t *testing.T, edits ...func(*oauth.Config)) *tokenFlow {
	t.Helper()
	tf := &tokenFlow{tokenDB: tokenSchema.Open(t)}
	tokens, err := apitoken.New(apitoken.Config{
		DB: tf.tokenDB, Prefix: "a",
		Now: func() time.Time { return tf.now },
		Grant: func(
			ctx context.Context, id string, access rastrillo.Access,
		) (*rastrillo.Grant, error) {
			return tf.srv.Grant(ctx, id, access)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tf.tokens = tokens
	use := func(config *oauth.Config) { config.Tokens = tokens }
	tf.flow = newFlow(t, "", nil,
		append([]func(*oauth.Config){use}, edits...)...)
	return tf
}

// nameClient renames the registered client the flow approves.
func (tf *tokenFlow) nameClient(name string) {
	tf.t.Helper()
	err := tf.cfg.Store.SaveClient(context.Background(), oauth.Client{
		ID: "reg", Name: name,
		Redirects:    []string{agentCB, queryCB, loopCB},
		RegisteredAt: t0, UsedAt: t0,
	})
	if err != nil {
		tf.t.Fatal(err)
	}
}

// approve has ana approve the agent with access, and returns the code.
func (tf *tokenFlow) approve(access string) string {
	tf.t.Helper()
	page := tf.show(params())
	form := tf.redirected(tf.answer(page.Pending, "approve", access), agentCB)
	return form.Get("code")
}

func exchangeForm(code string) url.Values {
	return url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"code_verifier": {verifier}, "client_id": {"reg"},
		"redirect_uri": {agentCB}, "resource": {mcpResource},
	}
}

func refreshForm(token string) url.Values {
	return url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {token},
		"client_id": {"reg"},
	}
}

func (tf *tokenFlow) post(
	path string, form url.Values,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	tf.mux.ServeHTTP(recorder, request)
	return recorder
}

func (tf *tokenFlow) token(form url.Values) *httptest.ResponseRecorder {
	return tf.post("/oauth/token", form)
}

type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// issued checks recorder issued tokens, and returns them.
func (tf *tokenFlow) issued(recorder *httptest.ResponseRecorder) tokenAnswer {
	tf.t.Helper()
	uncached(tf.t, recorder)
	if recorder.Code != http.StatusOK {
		tf.t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var tokens tokenAnswer
	if err := json.Unmarshal(recorder.Body.Bytes(), &tokens); err != nil {
		tf.t.Fatal(err)
	}
	return tokens
}

// refusedWith checks recorder is the OAuth error code, with status.
func (tf *tokenFlow) refusedWith(
	recorder *httptest.ResponseRecorder, status int, code string,
) {
	tf.t.Helper()
	uncached(tf.t, recorder)
	var answer struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	err := json.Unmarshal(recorder.Body.Bytes(), &answer)
	if err != nil || recorder.Code != status || answer.Error != code ||
		answer.Description == "" {
		tf.t.Fatalf("status %d, body %s; want %d %s",
			recorder.Code, recorder.Body, status, code)
	}
	if ct := recorder.Header().Get("Content-Type"); ct != "application/json" {
		tf.t.Errorf("Content-Type = %q", ct)
	}
}

func uncached(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if cc := recorder.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

// connect approves with access and exchanges the code.
func (tf *tokenFlow) connect(access string) tokenAnswer {
	tf.t.Helper()
	return tf.issued(tf.token(exchangeForm(tf.approve(access))))
}

func (tf *tokenFlow) verify(token string) (rastrillo.Actor, error) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return tf.tokens.Verify(context.Background(), request)
}

func (tf *tokenFlow) works(access string) {
	tf.t.Helper()
	if _, err := tf.verify(access); err != nil {
		tf.t.Fatalf("access token refused: %v", err)
	}
}

func (tf *tokenFlow) dead(access string) {
	tf.t.Helper()
	if _, err := tf.verify(access); !errors.Is(err, api.ErrUnauthorized) {
		tf.t.Fatalf("access token: %v, want ErrUnauthorized", err)
	}
}

func (tf *tokenFlow) ended() {
	tf.t.Helper()
	if cs := tf.connections(); len(cs) != 0 {
		tf.t.Fatalf("live connections %+v", cs)
	}
}

func TestExchange(t *testing.T) {
	ctx := context.Background()
	tf := newTokenFlow(t)
	tf.nameClient("Claude Code")
	tokens := tf.connect("write")
	if tokens.TokenType != "Bearer" || tokens.ExpiresIn != 3600 ||
		tokens.Scope != "read write" ||
		!regexp.MustCompile(`^rt-[A-Za-z0-9_-]{43}$`).
			MatchString(tokens.RefreshToken) {
		t.Errorf("answer %+v", tokens)
	}
	cs := tf.connections()
	actor, err := tf.verify(tokens.AccessToken)
	if err != nil || len(cs) != 1 {
		t.Fatalf("verify: %v; connections %+v", err, cs)
	}
	id := cs[0].ID
	if actor.Subject != "ana" || actor.Agent != "Claude Code" ||
		actor.Access != rastrillo.Write || actor.Grant == nil ||
		actor.Grant.ID != id || actor.Session != "oauth:"+id {
		t.Errorf("actor %+v", actor)
	}
	sum := sha256.Sum256([]byte(tokens.RefreshToken))
	connection, retired, err := tf.cfg.Store.RefreshConnection(
		ctx, sum[:], t0.Add(30*day-time.Second))
	if err != nil || retired || connection.ID != id {
		t.Errorf("refresh token: %+v, retired %v, %v", connection, retired, err)
	}
	_, _, err = tf.cfg.Store.RefreshConnection(ctx, sum[:], t0.Add(30*day))
	if !errors.Is(err, oauth.ErrNotFound) {
		t.Errorf("refresh token after 30 days: %v", err)
	}

	tf.nameClient("Clåude <3")
	tokens = tf.connect("read")
	actor, err = tf.verify(tokens.AccessToken)
	if err != nil || tokens.Scope != "read" || actor.Access != rastrillo.Read ||
		actor.Agent != "Agent" {
		t.Errorf("read: %+v, actor %+v, %v", tokens, actor, err)
	}
}

func TestExchangeCharsetAllowed(t *testing.T) {
	tf := newTokenFlow(t)
	request := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader(exchangeForm(tf.approve("read")).Encode()))
	request.Header.Set("Content-Type",
		"application/x-www-form-urlencoded; charset=utf-8")
	recorder := httptest.NewRecorder()
	tf.mux.ServeHTTP(recorder, request)
	tf.issued(recorder)
}

func TestExchangeEndedConnection(t *testing.T) {
	tf := newTokenFlow(t)
	code := tf.approve("write")
	cs := tf.connections()
	err := tf.srv.End(context.Background(), "ana", cs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	tf.refusedWith(tf.token(exchangeForm(code)),
		http.StatusBadRequest, "invalid_grant")
}

// exchangeRefused checks an exchange edited by edit is refused with
// code, and that the refusal spent nothing: the code still works for
// whoever holds its verifier, so a stolen code can't end the
// connection (OAuth 2.1 §7.5.3).
func exchangeRefused(
	t *testing.T, status int, code string, edit func(url.Values),
) {
	t.Helper()
	tf := newTokenFlow(t)
	form := exchangeForm(tf.approve("write"))
	edit(form)
	tf.refusedWith(tf.token(form), status, code)
	if len(tf.connections()) != 1 {
		t.Fatal("a refused exchange ended the connection")
	}
	tf.works(tf.issued(tf.token(exchangeForm(form.Get("code")))).AccessToken)
}

func TestExchangeWrongVerifier(t *testing.T) {
	exchangeRefused(t, http.StatusBadRequest, "invalid_grant",
		func(form url.Values) { form.Set("code_verifier", verifier+"x") })
	exchangeRefused(t, http.StatusBadRequest, "invalid_grant",
		func(form url.Values) { form.Del("code_verifier") })
}

func TestExchangeWrongRedirect(t *testing.T) {
	for _, redirect := range []string{queryCB, loopCB, "https://e.test/"} {
		exchangeRefused(t, http.StatusBadRequest, "invalid_grant",
			func(form url.Values) { form.Set("redirect_uri", redirect) })
	}
}

// OAuth 2.1 drops redirect_uri from the exchange; PKCE binds the code.
func TestExchangeWithoutRedirect(t *testing.T) {
	tf := newTokenFlow(t)
	form := exchangeForm(tf.approve("write"))
	form.Del("redirect_uri")
	tf.works(tf.issued(tf.token(form)).AccessToken)
}

// approveChallenge has ana approve a request carrying challenge, and
// returns the code.
func (tf *tokenFlow) approveChallenge(challenge string) string {
	tf.t.Helper()
	form := params()
	form.Set("code_challenge", challenge)
	page := tf.show(form)
	got := tf.redirected(tf.answer(page.Pending, "approve", "write"), agentCB)
	return got.Get("code")
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// A verifier is 43 to 128 unreserved characters (RFC 7636 §4.1), even
// when its hash matches: S256 of "" is itself a valid challenge.
func TestExchangeRefusesMalformedVerifier(t *testing.T) {
	for name, bad := range map[string]string{
		"missing":   "",
		"short":     strings.Repeat("a", 42),
		"long":      strings.Repeat("a", 129),
		"reserved":  strings.Repeat("a", 42) + "/",
		"non-ascii": strings.Repeat("a", 41) + "é",
	} {
		t.Run(name, func(t *testing.T) {
			tf := newTokenFlow(t)
			form := exchangeForm(tf.approveChallenge(s256(bad)))
			form.Set("code_verifier", bad)
			if bad == "" {
				form.Del("code_verifier")
			}
			tf.refusedWith(tf.token(form), http.StatusBadRequest,
				"invalid_grant")
		})
	}
	for _, good := range []string{
		strings.Repeat("a", 43), strings.Repeat("Z", 128),
		strings.Repeat("-._~", 11),
	} {
		tf := newTokenFlow(t)
		form := exchangeForm(tf.approveChallenge(s256(good)))
		form.Set("code_verifier", good)
		tf.issued(tf.token(form))
	}
}

func TestExchangeWrongClient(t *testing.T) {
	exchangeRefused(t, http.StatusBadRequest, "invalid_grant",
		func(form url.Values) { form.Set("client_id", "other") })
	exchangeRefused(t, http.StatusUnauthorized, "invalid_client",
		func(form url.Values) { form.Del("client_id") })
}

func TestExchangeWrongResource(t *testing.T) {
	for _, res := range []string{
		"https://a.test/api/v1", "https://elsewhere.test/api/v1/mcp", "",
	} {
		exchangeRefused(t, http.StatusBadRequest, "invalid_target",
			func(form url.Values) { form.Set("resource", res) })
	}
}

func TestExchangeExpiredCode(t *testing.T) {
	tf := newTokenFlow(t)
	code := tf.approve("write")
	tf.now = tf.now.Add(61 * time.Second)
	tf.refusedWith(tf.token(exchangeForm(code)),
		http.StatusBadRequest, "invalid_grant")
}

// A replay that fails the checks proves nothing about who sent it, so
// it ends nothing; only a replay that passes them does.
func TestCodeReplayWithoutVerifierKeepsConnection(t *testing.T) {
	tf := newTokenFlow(t)
	code := tf.approve("write")
	tokens := tf.issued(tf.token(exchangeForm(code)))
	form := exchangeForm(code)
	form.Set("code_verifier", verifier+"x")
	tf.refusedWith(tf.token(form), http.StatusBadRequest, "invalid_grant")
	tf.works(tokens.AccessToken)
	if len(tf.connections()) != 1 {
		t.Fatal("a replay with the wrong verifier ended the connection")
	}
}

// raceOrder picks which of two racing exchanges finishes first once
// both have read the code; free leaves it to the scheduler.
type raceOrder int

const (
	free raceOrder = iota
	// claimantFirst holds the second claim until the first exchange
	// has answered.
	claimantFirst
	// replayFirst holds the claimant before it reads its connection
	// until the replay has ended that connection.
	replayFirst
)

// codeRaceStore holds each Code read, while armed, until two have read
// the code, so both exchanges pass the checks before either claims it.
// It counts the claims that took the code, and holds back whichever
// exchange order says goes second until release is closed.
type codeRaceStore struct {
	oauth.Store
	order   raceOrder
	armed   atomic.Bool
	arrived sync.WaitGroup
	claims  atomic.Int32 // TakeCode calls so far
	took    atomic.Int32 // claims that took an untaken code
	held    atomic.Bool  // the claimant's Connection read was held
	release chan struct{}
}

func (store *codeRaceStore) Code(
	ctx context.Context, hash []byte, now time.Time,
) (oauth.Code, error) {
	code, err := store.Store.Code(ctx, hash, now)
	if store.armed.Load() {
		store.arrived.Done()
		store.arrived.Wait()
	}
	return code, err
}

func (store *codeRaceStore) TakeCode(
	ctx context.Context, hash []byte, now time.Time,
) (oauth.Code, bool, error) {
	second := store.claims.Add(1) == 2
	if second && store.order == claimantFirst {
		<-store.release
	}
	code, used, err := store.Store.TakeCode(ctx, hash, now)
	if err == nil && !used {
		store.took.Add(1)
	}
	return code, used, err
}

func (store *codeRaceStore) Connection(
	ctx context.Context, id string,
) (oauth.Connection, error) {
	if store.armed.Load() && store.order == replayFirst &&
		store.held.CompareAndSwap(false, true) {
		<-store.release
	}
	return store.Store.Connection(ctx, id)
}

// Two exchanges of one code that both pass the checks: exactly one
// claims it, and the other is a replay, which ends the connection.
// The claimant may answer with tokens, if it got them before the end,
// or invalid_grant, if the end came first; either way nothing it was
// given still works once the race settles.
func TestCodeExchangeRace(t *testing.T) {
	for name, order := range map[string]raceOrder{
		"free": free, "claimant first": claimantFirst,
		"replay first": replayFirst,
	} {
		t.Run(name, func(t *testing.T) { codeExchangeRace(t, order) })
	}
}

func codeExchangeRace(t *testing.T, order raceOrder) {
	race := &codeRaceStore{order: order, release: make(chan struct{})}
	tf := newTokenFlow(t, func(config *oauth.Config) {
		race.Store, config.Store = config.Store, race
	})
	code := tf.approve("write")
	race.arrived.Add(2)
	race.armed.Store(true)
	answers := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { answers <- tf.token(exchangeForm(code)) }()
	}
	first := <-answers
	close(race.release)
	second := <-answers
	race.armed.Store(false)

	if took := race.took.Load(); took != 1 {
		t.Fatalf("%d exchanges claimed the code, want exactly 1", took)
	}
	var issued []tokenAnswer
	for _, answer := range []*httptest.ResponseRecorder{first, second} {
		if answer.Code == http.StatusOK {
			issued = append(issued, tf.issued(answer))
			continue
		}
		tf.refusedWith(answer, http.StatusBadRequest, "invalid_grant")
	}
	want := map[raceOrder]int{claimantFirst: 1, replayFirst: 0}
	if expected, fixed := want[order]; fixed && len(issued) != expected ||
		len(issued) > 1 {
		t.Fatalf("%d exchanges got tokens", len(issued))
	}
	for _, tokens := range issued {
		tf.dead(tokens.AccessToken)
		tf.refusedWith(tf.token(refreshForm(tokens.RefreshToken)),
			http.StatusBadRequest, "invalid_grant")
	}
	tf.ended()
}

func TestCodeReplayEndsConnection(t *testing.T) {
	tf := newTokenFlow(t)
	code := tf.approve("write")
	tokens := tf.issued(tf.token(exchangeForm(code)))
	tf.works(tokens.AccessToken)
	tf.refusedWith(tf.token(exchangeForm(code)),
		http.StatusBadRequest, "invalid_grant")
	tf.dead(tokens.AccessToken)
	tf.refusedWith(tf.token(refreshForm(tokens.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
	tf.ended()
}

func TestRefreshRotates(t *testing.T) {
	tf := newTokenFlow(t)
	first := tf.connect("write")
	tf.now = tf.now.Add(2 * time.Hour)
	rotated := tf.issued(tf.token(refreshForm(first.RefreshToken)))
	if rotated.RefreshToken == first.RefreshToken ||
		rotated.AccessToken == first.AccessToken ||
		rotated.Scope != "read write" ||
		rotated.ExpiresIn != 3600 ||
		rotated.TokenType != "Bearer" {
		t.Errorf("rotated %+v", rotated)
	}
	tf.works(rotated.AccessToken)
	if cs := tf.connections(); !cs[0].LastUsed.Equal(tf.now) {
		t.Errorf("LastUsed = %v, want %v", cs[0].LastUsed, tf.now)
	}
	form := refreshForm(rotated.RefreshToken)
	form.Set("resource", mcpResource)
	form.Set("scope", "read")
	narrowed := tf.issued(tf.token(form))
	actor, err := tf.verify(narrowed.AccessToken)
	if err != nil || narrowed.Scope != "read" ||
		actor.Access != rastrillo.Read {
		t.Errorf("narrowed: %+v, actor %+v, %v", narrowed, actor, err)
	}
	if actor.Grant == nil ||
		!slices.Equal(actor.Grant.Scopes, []string{"read"}) {
		t.Errorf("narrowed token's grant %+v, want scopes [read]",
			actor.Grant)
	}
	widened := tf.issued(tf.token(refreshForm(narrowed.RefreshToken)))
	if widened.Scope != "read write" {
		t.Errorf("after a narrowed refresh: %+v", widened)
	}
}

func TestRefreshReuseEndsConnection(t *testing.T) {
	// The reuse is caught before any parameter is checked, so a
	// replay can't hide behind a wrong client, resource or scope.
	for _, edit := range []func(url.Values){
		func(url.Values) {},
		func(form url.Values) { form.Set("client_id", "other") },
		func(form url.Values) { form.Del("client_id") },
		func(form url.Values) { form.Set("resource", "https://e.test/") },
		func(form url.Values) { form.Set("scope", "write") },
	} {
		tf := newTokenFlow(t)
		first := tf.connect("read")
		rotated := tf.issued(tf.token(refreshForm(first.RefreshToken)))
		form := refreshForm(first.RefreshToken)
		edit(form)
		tf.refusedWith(tf.token(form), http.StatusBadRequest,
			"invalid_grant")
		tf.dead(first.AccessToken)
		tf.dead(rotated.AccessToken)
		tf.refusedWith(tf.token(refreshForm(rotated.RefreshToken)),
			http.StatusBadRequest, "invalid_grant")
		tf.ended()
	}
}

// A thief who rotates a stolen token keeps its line alive past the
// original's expiry. The person's client replaying the original then
// still ends the thief's connection.
func TestRefreshReplayAfterExpiryEndsConnection(t *testing.T) {
	tf := newTokenFlow(t)
	original := tf.connect("write")
	stolen := original
	for _, at := range []time.Duration{time.Hour, 29 * day, 31 * day} {
		tf.now = t0.Add(at)
		stolen = tf.issued(tf.token(refreshForm(stolen.RefreshToken)))
	}
	tf.now = t0.Add(31*day + time.Minute)
	tf.refusedWith(tf.token(refreshForm(original.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
	tf.dead(stolen.AccessToken)
	tf.ended()
}

// raceStore holds each RefreshConnection, while armed, until two have
// read the token, so both refreshes pass the reuse check before either
// rotates.
type raceStore struct {
	oauth.Store
	armed   atomic.Bool
	arrived sync.WaitGroup
}

func (race *raceStore) RefreshConnection(
	ctx context.Context, hash []byte, now time.Time,
) (oauth.Connection, bool, error) {
	connection, retired, err := race.Store.RefreshConnection(ctx, hash, now)
	if race.armed.Load() {
		race.arrived.Done()
		race.arrived.Wait()
	}
	return connection, retired, err
}

func TestRefreshRaceEndsConnection(t *testing.T) {
	race := &raceStore{}
	tf := newTokenFlow(t, func(config *oauth.Config) {
		race.Store, config.Store = config.Store, race
	})
	first := tf.connect("write")
	race.arrived.Add(2)
	race.armed.Store(true)
	var ws [2]*httptest.ResponseRecorder
	var done sync.WaitGroup
	for index := range ws {
		done.Add(1)
		go func() {
			defer done.Done()
			ws[index] = tf.token(refreshForm(first.RefreshToken))
		}()
	}
	done.Wait()
	race.armed.Store(false)
	won, lost := ws[0], ws[1]
	if won.Code != http.StatusOK {
		won, lost = lost, won
	}
	rotated := tf.issued(won)
	tf.refusedWith(lost, http.StatusBadRequest, "invalid_grant")
	tf.dead(first.AccessToken)
	tf.dead(rotated.AccessToken)
	tf.refusedWith(tf.token(refreshForm(rotated.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
	tf.ended()
}

// refreshRefused checks a refresh edited by edit is refused with code,
// and leaves the refresh token working, once.
func refreshRefused(
	t *testing.T, status int, code string, edit func(url.Values),
) {
	t.Helper()
	tf := newTokenFlow(t)
	first := tf.connect("read")
	form := refreshForm(first.RefreshToken)
	edit(form)
	tf.refusedWith(tf.token(form), status, code)
	tf.works(first.AccessToken)
	rotated := tf.issued(tf.token(refreshForm(first.RefreshToken)))
	tf.works(rotated.AccessToken)
}

func TestRefreshWrongClient(t *testing.T) {
	refreshRefused(t, http.StatusBadRequest, "invalid_grant",
		func(form url.Values) { form.Set("client_id", "other") })
	refreshRefused(t, http.StatusUnauthorized, "invalid_client",
		func(form url.Values) { form.Del("client_id") })
}

func TestRefreshForeignResource(t *testing.T) {
	for _, res := range []string{
		"https://a.test/api/v1", "https://elsewhere.test/api/v1/mcp",
	} {
		refreshRefused(t, http.StatusBadRequest, "invalid_target",
			func(form url.Values) { form.Set("resource", res) })
	}
}

func TestRefreshScopeExpansion(t *testing.T) {
	for _, scope := range []string{"write", "read write", "admin"} {
		refreshRefused(t, http.StatusBadRequest, "invalid_scope",
			func(form url.Values) { form.Set("scope", scope) })
	}
}

func TestRefreshExpires(t *testing.T) {
	tf := newTokenFlow(t)
	first := tf.connect("write")
	tf.now = tf.now.Add(30*day - time.Second)
	second := tf.issued(tf.token(refreshForm(first.RefreshToken)))
	// Each use renews it, so a connection in use outlives 30 days.
	tf.now = tf.now.Add(30*day - time.Second)
	third := tf.issued(tf.token(refreshForm(second.RefreshToken)))
	tf.now = tf.now.Add(30 * day)
	tf.refusedWith(tf.token(refreshForm(third.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
}

func TestRefreshNotAllowed(t *testing.T) {
	tf := newTokenFlow(t)
	tokens := tf.connect("write")
	tf.allowed = false
	tf.refusedWith(tf.token(refreshForm(tokens.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
	tf.allowed = true
	tf.issued(tf.token(refreshForm(tokens.RefreshToken)))
}

func TestAccessTokenExpires(t *testing.T) {
	tf := newTokenFlow(t)
	tokens := tf.connect("write")
	tf.now = tf.now.Add(time.Hour - time.Second)
	tf.works(tokens.AccessToken)
	tf.now = tf.now.Add(time.Second)
	tf.dead(tokens.AccessToken)
}

func TestTokenNoStoreAndErrors(t *testing.T) {
	tf := newTokenFlow(t)
	send := func(contentType, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost,
			"/oauth/token?grant_type=refresh_token",
			strings.NewReader(body))
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		recorder := httptest.NewRecorder()
		tf.mux.ServeHTTP(recorder, request)
		return recorder
	}
	form := "application/x-www-form-urlencoded"
	bad := http.StatusBadRequest
	tests := []struct {
		name, contentType, body string
		status                  int
		code                    string
	}{
		{"json", "application/json", `{"grant_type":"refresh_token"}`,
			bad, "invalid_request"},
		{"no type", "", "grant_type=refresh_token", bad, "invalid_request"},
		{"too big", form, "grant_type=refresh_token&pad=" +
			strings.Repeat("x", 64<<10), bad, "invalid_request"},
		{"query only", form, "", bad, "invalid_request"},
		{"unknown grant", form, "grant_type=password",
			bad, "unsupported_grant_type"},
		{"grant twice", form,
			"grant_type=refresh_token&grant_type=refresh_token",
			bad, "invalid_request"},
		{"no code", form, "grant_type=authorization_code",
			bad, "invalid_request"},
		{"no refresh token", form, "grant_type=refresh_token",
			bad, "invalid_request"},
		{"unknown code", form, "grant_type=authorization_code&code=x",
			bad, "invalid_grant"},
		{"unknown refresh token", form,
			"grant_type=refresh_token&refresh_token=rt-x&client_id=reg",
			bad, "invalid_grant"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tf.t = t
			tf.refusedWith(send(tt.contentType, tt.body),
				tt.status, tt.code)
		})
	}
	tf.t = t
	tokens := tf.connect("write")
	twice := refreshForm(tokens.RefreshToken)
	twice.Add("refresh_token", tokens.RefreshToken)
	tf.refusedWith(tf.token(twice), bad, "invalid_request")
	tf.issued(tf.token(refreshForm(tokens.RefreshToken)))
}

// failStore fails the refresh token writes the test sets it to fail.
type failStore struct {
	oauth.Store
	saveRefresh, rotate bool
}

var errFailed = errors.New("disk full")

func (store *failStore) SaveRefresh(
	ctx context.Context, refresh oauth.Refresh,
) error {
	if store.saveRefresh {
		return errFailed
	}
	return store.Store.SaveRefresh(ctx, refresh)
}

func (store *failStore) RotateRefresh(
	ctx context.Context, old []byte, next oauth.Refresh, now time.Time,
) (oauth.Connection, bool, error) {
	if store.rotate {
		return oauth.Connection{}, false, errFailed
	}
	return store.Store.RotateRefresh(ctx, old, next, now)
}

// liveTokens counts the access tokens of connections not yet revoked.
func (tf *tokenFlow) liveTokens() int {
	tf.t.Helper()
	var count int
	err := tf.tokenDB.QueryRow(`SELECT count(*) FROM api_tokens
WHERE connection != '' AND revoked_at IS NULL`).Scan(&count)
	if err != nil {
		tf.t.Fatal(err)
	}
	return count
}

// An access token minted for a rotation that then fails reaches nobody,
// so it is revoked.
func TestFailedRotationRevokesMintedToken(t *testing.T) {
	failing := &failStore{}
	tf := newTokenFlow(t, func(config *oauth.Config) {
		failing.Store, config.Store = config.Store, failing
	})
	first := tf.connect("write")
	failing.rotate = true
	tf.refusedWith(tf.token(refreshForm(first.RefreshToken)),
		http.StatusInternalServerError, "server_error")
	if live := tf.liveTokens(); live != 1 {
		t.Fatalf("%d live access tokens, want only the first", live)
	}
	failing.rotate = false
	tf.works(tf.issued(tf.token(refreshForm(first.RefreshToken))).AccessToken)
}

// A code is spent once claimed, so an exchange that fails after the
// claim leaves a connection that could never get a token: it is ended,
// not left on the person's list.
func TestFailedExchangeEndsConnection(t *testing.T) {
	failing := &failStore{}
	tf := newTokenFlow(t, func(config *oauth.Config) {
		failing.Store, config.Store = config.Store, failing
	})
	code := tf.approve("write")
	failing.saveRefresh = true
	tf.refusedWith(tf.token(exchangeForm(code)),
		http.StatusInternalServerError, "server_error")
	tf.ended()
	if live := tf.liveTokens(); live != 0 {
		t.Fatalf("%d live access tokens, want none", live)
	}
}
