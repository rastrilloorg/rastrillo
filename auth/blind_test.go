package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const blindCanary = "  Blind.Canary+NeverPersist@EXAMPLE.COM  "
const blindSubject = "person_opaque_7391"

type blindSQL struct{ database *sql.DB }

func (store *blindSQL) Put(ctx context.Context, link BlindLink) error {
	_, err := store.database.ExecContext(ctx, `INSERT INTO blind_links
 (hash, lookup_key, purpose, expires) VALUES (?, ?, ?, ?)`,
		link.Hash, link.LookupKey, link.Purpose, link.Expires.UnixNano())
	return err
}

func (store *blindSQL) Take(ctx context.Context, hash string) (
	BlindLink, bool, error,
) {
	var link BlindLink
	var expires int64
	err := store.database.QueryRowContext(ctx, `DELETE FROM blind_links
 WHERE hash = ? RETURNING hash, lookup_key, purpose, expires`, hash).
		Scan(&link.Hash, &link.LookupKey, &link.Purpose, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return link, false, nil
	}
	link.Expires = time.Unix(0, expires)
	return link, err == nil, err
}

func (store *blindSQL) Spend(ctx context.Context, key string) (int, error) {
	result, err := store.database.ExecContext(ctx,
		"DELETE FROM blind_links WHERE lookup_key = ?", key)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}

func (store *blindSQL) Sweep(ctx context.Context, now time.Time) error {
	_, err := store.database.ExecContext(ctx,
		"DELETE FROM blind_links WHERE expires <= ?", now.UnixNano())
	return err
}

type blindHarness struct {
	auth   *Auth
	config Config
	mail   *captureMailer
	store  *blindSQL
	path   string
	logs   bytes.Buffer
}

func blindKey(_ context.Context, address string) (string, error) {
	digest := hmac.New(sha256.New, []byte("consumer-owned-test-pepper"))
	digest.Write([]byte(strings.ToLower(strings.TrimSpace(address))))
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func newBlindHarness(t *testing.T, stock bool) *blindHarness {
	t.Helper()
	harness := &blindHarness{mail: &captureMailer{}}
	harness.path = filepath.Join(t.TempDir(), "blind.db")
	database, err := db.Open(harness.path, nil)
	requireBlindSuccess(t, err)
	t.Cleanup(func() { database.Close() })
	schema := sessions.Schema
	if stock {
		schema = migrate.Merge(schema, Schema)
	}
	_, err = migrate.Apply(context.Background(), database, schema)
	requireBlindSuccess(t, err)
	harness.store = &blindSQL{database.Writer()}
	_, err = harness.store.database.Exec(`CREATE TABLE blind_links (
 hash TEXT PRIMARY KEY, lookup_key TEXT NOT NULL,
 purpose TEXT NOT NULL, expires INTEGER NOT NULL)`)
	requireBlindSuccess(t, err)
	if _, err := harness.store.database.Exec(
		"PRAGMA wal_autocheckpoint = 0"); err != nil {
		t.Fatal(err)
	}
	harness.config = Config{
		DB: database.Writer(), Origin: "http://app.test",
		InstanceKey: "blind-test-instance-key", ProofOff: true,
		Mailer: harness.mail,
		Logger: slog.New(slog.NewTextHandler(&harness.logs,
			&slog.HandlerOptions{Level: slog.LevelDebug})),
		BlindLinks: &BlindLinks{
			Store: harness.store, LookupKey: blindKey,
			ResolveSubject: func(
				_ context.Context, key string,
			) (string, error) {
				want, _ := blindKey(
					context.Background(), blindCanary,
				)
				if key != want {
					return "", errors.New("unknown account")
				}
				return blindSubject, nil
			},
		},
	}
	previous := slog.Default()
	writer, flags := log.Writer(), log.Flags()
	slog.SetDefault(harness.config.Logger)
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	harness.auth, err = New(harness.config)
	requireBlindSuccess(t, err)
	return harness
}

func (harness *blindHarness) send(t *testing.T) string {
	t.Helper()
	response := beginSignin(t, harness.auth, blindCanary)
	if response.Header().Get("Location") != "/signin?sent=1" {
		t.Fatalf("Begin failed: %d %s", response.Code, response.Body)
	}
	assertBlindResponse(t, response)
	assertBlindBytes(t, "mailed link body", []byte(harness.mail.sentBody()))
	link := linkRE.FindString(harness.mail.sentBody())
	if link == "" {
		t.Fatal("no mailed link")
	}
	return link
}

func assertBlindBytes(t *testing.T, name string, data []byte) {
	t.Helper()
	folded := bytes.ToLower(data)
	for _, needle := range blindNeedles() {
		if bytes.Contains(folded, bytes.ToLower([]byte(needle))) {
			t.Errorf("address leaked into %s", name)
		}
	}
}

func (harness *blindHarness) scan(t *testing.T) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(harness.path + suffix)
		requireBlindSuccess(t, err)
		if len(data) == 0 {
			t.Fatalf("empty persistence artifact %s", suffix)
		}
		assertBlindBytes(t, "SQLite"+suffix, data)
	}
	assertBlindBytes(t, "logs", harness.logs.Bytes())
}

func TestBlindLinksNeverPersistAnAddress(t *testing.T) {
	harness := newBlindHarness(t, true)
	link := harness.send(t)
	harness.scan(t)
	wantTo := strings.TrimSpace(blindCanary)
	if !strings.EqualFold(harness.mail.sentTo(), wantTo) {
		t.Fatal("delivery did not receive the transient address")
	}
	var record BlindLink
	err := harness.store.database.QueryRow(`SELECT hash, lookup_key,
 purpose FROM blind_links`).Scan(&record.Hash, &record.LookupKey,
		&record.Purpose)
	requireBlindSuccess(t, err)
	token := redeemRequest(t, link).FormValue("token")
	key, _ := blindKey(context.Background(), blindCanary)
	if record.Hash != HashToken(token) || record.LookupKey != key {
		t.Fatal("storage did not receive token hash and opaque lookup key")
	}
	fresh, err := New(harness.config)
	requireBlindSuccess(t, err)
	response := redeemBlind(t, fresh, link)
	requireBlindSession(t, fresh, response)
	harness.scan(t)
	if _, err := harness.store.database.Exec(
		"PRAGMA wal_checkpoint(FULL)"); err != nil {
		t.Fatal(err)
	}
	harness.scan(t)
	assertBlindTables(t, harness)
}

func requireBlindSession(t *testing.T, auth *Auth,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/" {
		t.Fatalf("no sign-in: %d %s", response.Code, response.Body)
	}
	request := httptest.NewRequest("GET", "/", nil)
	assertBlindResponse(t, response)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name != auth.SessionCookie() && cookie.Value != "" {
			t.Errorf("unexpected persistent cookie: %s", cookie.Name)
		}
		request.AddCookie(cookie)
	}
	identity, ok := auth.SessionFrom(request)
	if !ok || identity.Address != blindSubject {
		t.Fatalf("session subject = %q, present = %v", identity.Address, ok)
	}
}

func TestBlindLinksWorkWithoutTheStockAuthSchema(t *testing.T) {
	harness := newBlindHarness(t, false)
	link := harness.send(t)
	landing := httptest.NewRecorder()
	harness.auth.Verify(landing, httptest.NewRequest("GET", link, nil))
	if landing.Code != http.StatusOK || landing.Body.Len() == 0 {
		t.Fatal("confirm GET failed")
	}
	assertBlindResponse(t, landing)

	response := redeemBlind(t, harness.auth, link)
	requireBlindSession(t, harness.auth, response)
	harness.scan(t)
}

func TestBlindLinksRejectWrongPurposeExpiredAndReusedTokens(t *testing.T) {
	guards := []string{
		"purpose", "expired", "boundary", "reuse", "clock",
	}
	for _, guard := range guards {
		t.Run(guard, func(t *testing.T) {
			harness := newBlindHarness(t, true)
			link := harness.send(t)
			switch guard {
			case "clock":
				harness.auth.now = func() time.Time {
					return time.Now().Add(LinkTTL + time.Second)
				}
			case "purpose":
				_, err := harness.store.database.Exec(
					"UPDATE blind_links SET purpose = 'password-reset'")
				requireBlindSuccess(t, err)
			case "expired", "boundary":
				now := time.Now().Add(-time.Hour)
				harness.auth.now = func() time.Time { return now }
				expiry := now
				if guard == "expired" {
					expiry = now.Add(-time.Second)
				}
				_, err := harness.store.database.Exec(
					"UPDATE blind_links SET expires = ?", expiry.UnixNano())
				requireBlindSuccess(t, err)
			case "reuse":
				response := redeemBlind(t, harness.auth, link)
				requireBlindSession(t, harness.auth, response)
			}
			response := redeemBlind(t, harness.auth, link)
			location := response.Header().Get("Location")
			if !strings.Contains(location, "err=expired") {
				t.Fatalf("%s link accepted: %d %s", guard, response.Code,
					response.Header().Get("Location"))
			}
			for _, cookie := range response.Result().Cookies() {
				isSession := cookie.Name == harness.auth.SessionCookie()
				if isSession && cookie.Value != "" {
					t.Fatal("invalid link minted a session")
				}
			}
		})
	}
}

func TestBlindLinksResolveBeforeSecondFactor(t *testing.T) {
	harness := newBlindHarness(t, true)
	seen := false
	harness.auth.cfg.SecondFactor = func(_ http.ResponseWriter,
		_ *http.Request, session sessions.Session) (bool, error) {
		seen = true
		if session.Subject != blindSubject {
			t.Errorf("second factor received %q", session.Subject)
		}
		return true, nil
	}
	redeemBlind(t, harness.auth, harness.send(t))
	if !seen {
		t.Fatal("second factor was not reached")
	}
}

func TestBlindLinksResolutionFailsClosed(t *testing.T) {
	subjects := []string{
		"", " ", blindCanary, strings.TrimSpace(blindCanary),
		"error", "value-and-error",
	}
	for _, subject := range subjects {
		t.Run(fmt.Sprintf("subject=%q", subject), func(t *testing.T) {
			harness := newBlindHarness(t, true)
			harness.config.BlindLinks.ResolveSubject = func(context.Context,
				string) (string, error) {
				if subject == "value-and-error" {
					return blindSubject, errors.New(blindCanary)
				}
				if subject == "error" {
					return "", errors.New(blindCanary)
				}
				return subject, nil
			}
			harness.configure(t)
			link := harness.send(t)
			assertNoBlindCookies(t, redeemBlind(t, harness.auth, link))
			var count int
			if err := harness.store.database.QueryRow(
				"SELECT count(*) FROM sessions").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("failed resolver minted a session")
			}
			response := redeemBlind(t, harness.auth, link)
			location := response.Header().Get("Location")
			if !strings.Contains(location, "err=expired") {
				t.Fatal("failed resolver left a reusable token")
			}
			harness.scan(t)
		})
	}
}

func blindNeedles() []string {
	trimmed := strings.TrimSpace(blindCanary)
	local, domain, _ := strings.Cut(trimmed, "@")
	bare, _, _ := strings.Cut(local, "+")
	variants := []string{blindCanary, trimmed, strings.ToLower(blindCanary),
		strings.ToLower(trimmed), local, bare, bare + "@" + domain}
	var needles []string
	for _, variant := range variants {
		raw := []byte(variant)
		needles = append(needles, variant, url.QueryEscape(variant),
			url.PathEscape(variant), hex.EncodeToString(raw),
			base64.StdEncoding.EncodeToString(raw),
			base64.RawURLEncoding.EncodeToString(raw))
	}
	return needles
}

func assertBlindResponse(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	assertBlindBytes(t, "HTTP headers", []byte(fmt.Sprint(response.Header())))
	assertBlindBytes(t, "HTTP body", response.Body.Bytes())
}

func assertBlindTables(t *testing.T, harness *blindHarness) {
	t.Helper()
	var legacy int
	err := harness.store.database.QueryRow(`SELECT
 (SELECT count(*) FROM auth_links) +
 (SELECT count(*) FROM auth_sessions)`).Scan(&legacy)
	requireBlindSuccess(t, err)
	if legacy != 0 {
		t.Fatal("stock auth tables received records")
	}
	var columns string
	err = harness.store.database.QueryRow(`SELECT group_concat(name, ',')
 FROM pragma_table_info('sessions')`).Scan(&columns)
	requireBlindSuccess(t, err)
	want := "token_hash,subject,method,auth_time,created_at,expires_at"
	if columns != want {
		t.Fatalf("unexpected session fields: %s", columns)
	}
}

func (harness *blindHarness) configure(t *testing.T) {
	t.Helper()
	auth, err := New(harness.config)
	requireBlindSuccess(t, err)
	harness.auth = auth
}

func requireBlindSuccess(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func redeemBlind(
	t *testing.T, auth *Auth, link string,
) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	auth.Verify(response, redeemRequest(t, link))
	return response
}

func assertNoBlindCookies(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	assertBlindResponse(t, response)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Value != "" {
			t.Fatalf("unexpected cookie value: %s", cookie.Name)
		}
	}
}
