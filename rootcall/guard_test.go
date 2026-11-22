package rootcall_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/rootcall"
)

const (
	rootOrigin     = "https://root.example.com"
	instanceOrigin = "https://box1.example.net"
	planURL        = instanceOrigin + "/_carlos/slopbox/plan"
	planBody       = `{"plan":"paid","seq":3}`
)

var schema = dbtest.FromSet(rootcall.Schema)

func TestMain(main *testing.M) {
	code := main.Run()
	schema.Remove()
	os.Exit(code)
}

var epoch = time.Unix(1700000000, 0)

type fixture struct {
	key    *crypto.Keypair
	db     *sql.DB
	guard  *rootcall.Guard
	now    time.Time
	log    *bytes.Buffer
	served []string // the bodies the wrapped handler read
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	fix := &fixture{key: key, db: schema.Open(t), now: epoch,
		log: &bytes.Buffer{}}
	fix.guard = fix.process(t)
	return fix
}

// process is a guard sharing this fixture's database and clock, as
// another process of the same app would.
func (fix *fixture) process(t *testing.T) *rootcall.Guard {
	t.Helper()
	guard, err := rootcall.New(rootcall.Config{
		Root: rootOrigin, Audience: instanceOrigin, DB: fix.db,
		Keys:    map[string][]byte{"key_1": fix.key.SignPub()},
		MaxBody: 64,
		Now:     func() time.Time { return fix.now },
		Logger:  slog.New(slog.NewTextHandler(fix.log, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return guard
}

func (fix *fixture) caller() rootcall.Caller {
	return rootcall.Caller{Origin: rootOrigin, Key: fix.key,
		KeyID: "key_1", Now: func() time.Time { return fix.now }}
}

// call builds a signed request and sends it through the wire format, so
// the instance sees what a real server parses off the connection.
func (fix *fixture) call(
	t *testing.T, method, url string, body string,
) *http.Request {
	t.Helper()
	request, err := fix.caller().NewRequest(
		context.Background(), method, url, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return overTheWire(t, request)
}

func overTheWire(t *testing.T, request *http.Request) *http.Request {
	t.Helper()
	var wire bytes.Buffer
	if err := request.Write(&wire); err != nil {
		t.Fatal(err)
	}
	received, err := http.ReadRequest(bufio.NewReader(&wire))
	if err != nil {
		t.Fatal(err)
	}
	return received
}

func (fix *fixture) serve(
	guard *rootcall.Guard, request *http.Request,
) *httptest.ResponseRecorder {
	handler := guard.Wrap(http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		if _, ok := rootcall.FromContext(request.Context()); !ok {
			http.Error(response, "no call", http.StatusTeapot)
			return
		}
		body, _ := io.ReadAll(request.Body)
		fix.served = append(fix.served, string(body))
		response.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func (fix *fixture) status(request *http.Request) int {
	return fix.serve(fix.guard, request).Code
}

func TestGuardRunsTheHandlerForAGoodCall(t *testing.T) {
	fix := newFixture(t)
	if code := fix.status(fix.call(t, "POST", planURL, planBody)); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
	if len(fix.served) != 1 || fix.served[0] != planBody {
		t.Fatalf("handler read %q, want the signed body", fix.served)
	}
}

func TestGuardAdmitsAGetWithAQuery(t *testing.T) {
	fix := newFixture(t)
	url := instanceOrigin + "/_carlos/slopbox/usage?since=2026-11-01&x=a%2Fb"
	if code := fix.status(fix.call(t, "GET", url, "")); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
}

func TestGuardRefusesAReusedCall(t *testing.T) {
	fix := newFixture(t)
	request := fix.call(t, "POST", planURL, planBody)
	again := request.Clone(context.Background())
	again.Body = io.NopCloser(strings.NewReader(planBody))
	if code := fix.status(request); code != 204 {
		t.Fatalf("first status = %d, want 204", code)
	}
	if code := fix.status(again); code != 403 {
		t.Fatalf("replay status = %d, want 403", code)
	}
	if len(fix.served) != 1 {
		t.Fatalf("handler ran %d times, want once", len(fix.served))
	}
	if !strings.Contains(fix.log.String(), rootcall.ErrReplay.Error()) {
		t.Errorf("log = %q, want the replay named", fix.log)
	}
}

func TestGuardRefusesAReplayInAnotherProcess(t *testing.T) {
	fix := newFixture(t)
	other := fix.process(t)
	request := fix.call(t, "POST", planURL, planBody)
	again := request.Clone(context.Background())
	again.Body = io.NopCloser(strings.NewReader(planBody))
	if code := fix.status(request); code != 204 {
		t.Fatalf("first status = %d, want 204", code)
	}
	if code := fix.serve(other, again).Code; code != 403 {
		t.Fatalf("replay in another process = %d, want 403", code)
	}
}

// tampered sends a genuine call with one part of the request changed.
func (fix *fixture) tampered(
	t *testing.T, change func(*http.Request),
) (int, *http.Request) {
	t.Helper()
	request := fix.call(t, "POST", planURL, planBody)
	genuine := request.Clone(context.Background())
	genuine.Body = io.NopCloser(strings.NewReader(planBody))
	change(request)
	return fix.status(request), genuine
}

func TestGuardRefusesATamperedCallWithoutSpendingIt(t *testing.T) {
	cases := map[string]func(*http.Request){
		"body": func(request *http.Request) {
			request.Body = io.NopCloser(strings.NewReader(
				`{"plan":"paid","seq":9}`))
		},
		"method": func(request *http.Request) { request.Method = "PUT" },
		"path": func(request *http.Request) {
			request.RequestURI = "/_carlos/slopbox/pause"
		},
		"query": func(request *http.Request) {
			request.RequestURI += "?plan=trial"
		},
	}
	for name, change := range cases {
		fix := newFixture(t)
		code, genuine := fix.tampered(t, change)
		if code != 403 {
			t.Errorf("%s: status = %d, want 403", name, code)
		}
		if len(fix.served) != 0 {
			t.Errorf("%s: the handler ran", name)
		}
		if code := fix.status(genuine); code != 204 {
			t.Errorf("%s: genuine call after it = %d, want 204", name, code)
		}
	}
}

func TestGuardRefusesAnExpiredCall(t *testing.T) {
	fix := newFixture(t)
	request := fix.call(t, "POST", planURL, planBody)
	fix.now = epoch.Add(60 * time.Second)
	if code := fix.status(request); code != 403 {
		t.Fatalf("status = %d, want 403", code)
	}
}

func TestGuardRefusesACallSignedForAnotherInstance(t *testing.T) {
	fix := newFixture(t)
	request := fix.call(t, "POST",
		"https://box2.example.net/_carlos/slopbox/plan", planBody)
	request.Host = "box1.example.net"
	if code := fix.status(request); code != 403 {
		t.Fatalf("status = %d, want 403", code)
	}
}

func TestGuardIsNotFoundOnAnotherHost(t *testing.T) {
	fix := newFixture(t)
	request := fix.call(t, "POST", planURL, planBody)
	request.Host = "blog.example.org"
	if code := fix.status(request); code != 404 {
		t.Fatalf("status = %d, want 404", code)
	}
}

func TestGuardRefusesARequestWithoutACall(t *testing.T) {
	fix := newFixture(t)
	good := fix.call(t, "POST", planURL, planBody)
	token := strings.TrimPrefix(good.Header.Get("Authorization"),
		"Rootcall ")
	cases := map[string][]string{
		"none":          nil,
		"bearer":        {"Bearer " + token},
		"no token":      {"Rootcall "},
		"two headers":   {"Rootcall " + token, "Rootcall " + token},
		"not a token":   {"Rootcall nonsense"},
		"handoff token": {"Rootcall " + fix.handoffToken(t)},
	}
	for name, headers := range cases {
		request := fix.call(t, "POST", planURL, planBody)
		request.Header.Del("Authorization")
		for _, header := range headers {
			request.Header.Add("Authorization", header)
		}
		if code := fix.status(request); code != 403 {
			t.Errorf("%s: status = %d, want 403", name, code)
		}
	}
	if code := fix.status(good); code != 204 {
		t.Errorf("the genuine call after them = %d, want 204", code)
	}
}

func TestGuardTakesTheSchemeInAnyCase(t *testing.T) {
	fix := newFixture(t)
	request := fix.call(t, "POST", planURL, planBody)
	request.Header.Set("Authorization", "rootCALL "+strings.TrimPrefix(
		request.Header.Get("Authorization"), "Rootcall "))
	if code := fix.status(request); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
}

// handoffToken is a valid handoff assertion from the same root key, which
// must not pass as a call.
func (fix *fixture) handoffToken(t *testing.T) string {
	t.Helper()
	id, _ := assertion.RandomID()
	nonce, _ := assertion.RandomID()
	token, err := assertion.Sign(fix.key, assertion.Claims{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		Subject: "person_1", KeyID: "key_1", ID: id, Nonce: nonce,
		IssuedAt: epoch.Unix(), ExpiresAt: epoch.Unix() + 60,
		AuthTime: epoch.Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestGuardRefusesABodyOverTheLimit(t *testing.T) {
	fix := newFixture(t)
	request := fix.call(t, "POST", planURL, strings.Repeat("a", 65))
	if code := fix.status(request); code != 413 {
		t.Fatalf("status = %d, want 413", code)
	}
}

func TestGuardComparesTheRequestLineUnderStripPrefix(t *testing.T) {
	fix := newFixture(t)
	handler := http.StripPrefix("/_carlos", fix.guard.Wrap(
		http.HandlerFunc(func(response http.ResponseWriter,
			_ *http.Request) {
			response.WriteHeader(http.StatusNoContent)
		})))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, fix.call(t, "POST", planURL, planBody))
	if response.Code != 204 {
		t.Fatalf("status = %d, want 204", response.Code)
	}
}

func (fix *fixture) spentRows(t *testing.T) int {
	t.Helper()
	var rows int
	err := fix.db.QueryRow(`SELECT count(*) FROM rootcall_used`).Scan(&rows)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestGuardSweepsCallsTwoMinutesPastExpiry(t *testing.T) {
	fix := newFixture(t)
	fix.status(fix.call(t, "POST", planURL, planBody)) // expires epoch+60
	fix.now = epoch.Add(180 * time.Second)
	fix.status(fix.call(t, "POST", planURL, planBody))
	if rows := fix.spentRows(t); rows != 2 {
		t.Fatalf("rows = %d, want 2: swept inside the margin", rows)
	}
	fix.now = epoch.Add(181 * time.Second)
	fix.status(fix.call(t, "POST", planURL, planBody))
	if rows := fix.spentRows(t); rows != 2 {
		t.Fatalf("rows = %d, want 2: the first call was not swept", rows)
	}
}

func TestGuardAnswers500WhenItCannotSpendTheCall(t *testing.T) {
	fix := newFixture(t)
	if _, err := fix.db.Exec(`DROP TABLE rootcall_used`); err != nil {
		t.Fatal(err)
	}
	if code := fix.status(fix.call(t, "POST", planURL, planBody)); code != 500 {
		t.Fatalf("status = %d, want 500", code)
	}
	if len(fix.served) != 0 {
		t.Fatal("the handler ran for a call that was not spent")
	}
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	fix := newFixture(t)
	keys := map[string][]byte{"key_1": fix.key.SignPub()}
	cases := map[string]rootcall.Config{
		"no database": {Root: rootOrigin, Audience: instanceOrigin,
			Keys: keys},
		"no keys":     {Root: rootOrigin, Audience: instanceOrigin, DB: fix.db},
		"no audience": {Root: rootOrigin, Keys: keys, DB: fix.db},
		"negative limit": {Root: rootOrigin, Audience: instanceOrigin,
			Keys: keys, DB: fix.db, MaxBody: -1},
	}
	for name, config := range cases {
		if _, err := rootcall.New(config); !errors.Is(err, rootcall.ErrConfig) {
			t.Errorf("%s: got %v, want ErrConfig", name, err)
		}
	}
}

func TestCallerRefusesATargetThatIsNotAnInstanceOrigin(t *testing.T) {
	fix := newFixture(t)
	for _, url := range []string{
		"http://box1.example.net/x",
		"https://user@box1.example.net/x",
		"https://127.0.0.1/x",
		"https://BOX1.example.net/x",
		"https://root.example.com/x",
	} {
		_, err := fix.caller().NewRequest(
			context.Background(), "POST", url, nil)
		if err == nil {
			t.Errorf("%s: NewRequest signed it", url)
		}
	}
}

func TestGuardTakesAnAbsoluteFormTarget(t *testing.T) {
	fix := newFixture(t)
	signed := fix.call(t, "POST", planURL, planBody)
	request := httptest.NewRequest("POST", planURL,
		strings.NewReader(planBody))
	request.Header.Set("Authorization", signed.Header.Get("Authorization"))
	if code := fix.status(request); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
}

// stalledBody blocks its first Read until release closes, after telling
// reading that the guard has reached it.
type stalledBody struct {
	reading chan struct{}
	release chan struct{}
	body    io.Reader
	once    bool
}

func (stalled *stalledBody) Read(buffer []byte) (int, error) {
	if !stalled.once {
		stalled.once = true
		close(stalled.reading)
		<-stalled.release
	}
	return stalled.body.Read(buffer)
}

func TestGuardRefusesACopyWhoseBodyStalledPastTheSweep(t *testing.T) {
	fix := newFixture(t)
	genuine := fix.call(t, "POST", planURL, planBody)
	stalled := genuine.Clone(context.Background())
	body := &stalledBody{reading: make(chan struct{}),
		release: make(chan struct{}), body: strings.NewReader(planBody)}
	stalled.Body = io.NopCloser(body)
	done := make(chan int)
	go func() { done <- fix.serve(fix.guard, stalled).Code }()
	<-body.reading
	if code := fix.status(genuine); code != 204 {
		t.Fatalf("genuine status = %d, want 204", code)
	}
	// Past expiry and the sweep margin, a later call forgets the ID.
	fix.now = epoch.Add(181 * time.Second)
	if code := fix.status(fix.call(t, "POST", planURL, planBody)); code != 204 {
		t.Fatalf("later call status = %d, want 204", code)
	}
	close(body.release)
	if code := <-done; code != 403 {
		t.Fatalf("stalled copy status = %d, want 403", code)
	}
	if len(fix.served) != 2 {
		t.Fatalf("handler ran %d times, want 2", len(fix.served))
	}
}

func TestGuardRefusesAnAbsoluteTargetWhosePathIsInTheQuery(t *testing.T) {
	fix := newFixture(t)
	signed := fix.call(t, "POST", instanceOrigin+"/signed", planBody)
	cases := []string{
		"https://box1.example.net?operation=other&x=/signed",
		"https://box1.example.net#/signed",
		"https://box1.example.net",
	}
	for _, target := range cases {
		raw := "POST " + target + " HTTP/1.1\r\nHost: box1.example.net\r\n" +
			"Authorization: " + signed.Header.Get("Authorization") +
			"\r\nContent-Length: 23\r\n\r\n" + planBody
		request, err := http.ReadRequest(bufio.NewReader(
			strings.NewReader(raw)))
		if err != nil {
			continue // refused by the parser: nothing reaches the guard
		}
		if code := fix.status(request); code != 403 {
			t.Errorf("%s: status = %d, want 403", target, code)
		}
	}
	if len(fix.served) != 0 {
		t.Fatalf("the handler ran for %q", fix.served)
	}
}

func TestGuardTakesAnAbsoluteTargetWithEscapes(t *testing.T) {
	fix := newFixture(t)
	url := instanceOrigin + "/_carlos/a%2Fb/usage?since=2026-11-01&q=%2F"
	signed := fix.call(t, "GET", url, "")
	request := httptest.NewRequest("GET", url, nil)
	request.Header.Set("Authorization", signed.Header.Get("Authorization"))
	if code := fix.status(request); code != 204 {
		t.Fatalf("status = %d, want 204", code)
	}
}
