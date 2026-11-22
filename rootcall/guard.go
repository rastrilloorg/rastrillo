package rootcall

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/migrate"
)

// Scheme is the Authorization scheme a call's token rides in:
// "Authorization: Rootcall <token>". Go's client drops Authorization on a
// redirect to another host, which a header of our own would not get.
const Scheme = "Rootcall"

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema holds the table that remembers spent call IDs. Apply it with
// migrate.Apply alongside the app's own migrations.
var Schema = migrate.MustFromFS(migrationFS, "rootcall")

// These carry no submitted values either, so a refusal is logged as is.
var (
	ErrMissing = errors.New("rootcall: no Rootcall credential")
	ErrBinding = errors.New("rootcall: method, target or body not signed")
	ErrReplay  = errors.New("rootcall: call already used")

	errTooLarge = errors.New("rootcall: body over Config.MaxBody")
	errBody     = errors.New("rootcall: unreadable body")
	errInternal = errors.New("rootcall: internal")
)

const defaultMaxBody = 1 << 20

// sweepMargin keeps a spent ID this long past its expiry, so a second
// process whose clock runs up to two minutes behind still refuses it.
const sweepMargin = maxLifetime * time.Second

type Config struct {
	Root string            // instance.Setup.Root
	Keys map[string][]byte // instance.Setup.Keys
	// Audience is the instance's platform origin, https:// and
	// carlos.Host(), where the root sends calls, as for roster.
	Audience string
	DB       *sql.DB
	MaxBody  int64 // default 1 MiB
	Now      func() time.Time
	Logger   *slog.Logger
}

// Guard checks root calls before the handlers it wraps.
type Guard struct {
	verifier *Verifier
	db       *sql.DB
	maxBody  int64
	now      func() time.Time
	logger   *slog.Logger
}

func New(config Config) (*Guard, error) {
	if config.DB == nil || config.MaxBody < 0 {
		return nil, ErrConfig
	}
	verifier, err := NewVerifier(config.Root, config.Audience, config.Keys)
	if err != nil {
		return nil, ErrConfig
	}
	guard := &Guard{
		verifier: verifier, db: config.DB, maxBody: config.MaxBody,
		now: config.Now, logger: config.Logger,
	}
	if guard.maxBody == 0 {
		guard.maxBody = defaultMaxBody
	}
	if guard.now == nil {
		guard.now = time.Now
	}
	if guard.logger == nil {
		guard.logger = slog.Default()
	}
	return guard, nil
}

type claimsKey struct{}

// FromContext returns the call a Guard admitted, so a handler can refuse
// a request that reached it without one.
func FromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(Claims)
	return claims, ok
}

// Wrap runs next only for a call the root signed for this method, target
// and body, on the audience's host, unexpired and not seen before. The
// body next reads is the one that was checked.
func (guard *Guard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		// Roster's rule: the root calls the audience origin, so no other
		// host is answered.
		if !basepath.SameHost(request.Host, guard.verifier.audience) {
			http.NotFound(response, request)
			return
		}
		claims, err := guard.admit(response, request)
		if err != nil {
			guard.refuse(response, err)
			return
		}
		ctx := context.WithValue(request.Context(), claimsKey{}, claims)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

// admit runs every check before spending the call's ID, so a forged or
// altered request cannot burn a genuine one.
func (guard *Guard) admit(
	response http.ResponseWriter, request *http.Request,
) (Claims, error) {
	token, err := credential(request)
	if err != nil {
		return Claims{}, err
	}
	claims, err := guard.verifier.Verify(token, guard.now())
	if err != nil {
		return Claims{}, err
	}
	body, err := guard.readBody(response, request)
	if err != nil {
		return Claims{}, err
	}
	if !binds(claims, request, body) {
		return Claims{}, ErrBinding
	}
	if err := guard.spend(request.Context(), claims); err != nil {
		return Claims{}, err
	}
	// Checked again after the spend: reading a stalled body, or waiting on
	// the database, can outlast the call and the sweep that forgot its ID.
	// An ID spent before expiry cannot have been swept yet.
	if claims.ExpiresAt <= guard.now().Unix() {
		return Claims{}, ErrExpired
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	return claims, nil
}

// credential takes exactly one Authorization header. The scheme is
// case-insensitive (RFC 9110), the token is not.
func credential(request *http.Request) (string, error) {
	headers := request.Header.Values("Authorization")
	if len(headers) != 1 {
		return "", ErrMissing
	}
	scheme, token, found := strings.Cut(headers[0], " ")
	if !found || !strings.EqualFold(scheme, Scheme) || token == "" {
		return "", ErrMissing
	}
	return token, nil
}

func (guard *Guard) readBody(
	response http.ResponseWriter, request *http.Request,
) ([]byte, error) {
	limited := http.MaxBytesReader(response, request.Body, guard.maxBody)
	body, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, errTooLarge
		}
		return nil, errBody
	}
	return body, nil
}

// binds compares the request line as received: RequestURI is what the
// root's client wrote there, and StripPrefix and basepath leave it alone.
func binds(claims Claims, request *http.Request, body []byte) bool {
	hash := BodyHash(body)
	return request.Method == claims.Method &&
		originForm(request.RequestURI) == claims.Target &&
		subtle.ConstantTimeCompare([]byte(hash),
			[]byte(claims.BodyHash)) == 1
}

// originForm drops the scheme and host from an absolute-form target, which
// a proxy or httptest.NewRequest sends. The host is not lost: the server
// takes Request.Host from it, and Wrap has checked that already. The
// authority must end at a slash: one ending at ? would let a slash inside
// the query pass for the path.
func originForm(target string) string {
	if strings.HasPrefix(target, "/") {
		return target
	}
	rest, found := strings.CutPrefix(target, "https://")
	if !found {
		rest, found = strings.CutPrefix(target, "http://")
	}
	end := strings.IndexAny(rest, "/?#")
	if !found || end < 0 || rest[end] != '/' {
		return ""
	}
	return rest[end:]
}

// spend records the call's ID. A second insert of the same ID changes no
// row, which is how a replay shows itself, in this process or another.
func (guard *Guard) spend(ctx context.Context, claims Claims) error {
	result, err := guard.db.ExecContext(ctx,
		`INSERT INTO rootcall_used (jti, expires_at) VALUES (?, ?)
		 ON CONFLICT DO NOTHING`, claims.ID, claims.ExpiresAt)
	if err != nil {
		return fmt.Errorf("%w: spend: %w", errInternal, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: spend: %w", errInternal, err)
	}
	if inserted == 0 {
		return ErrReplay
	}
	guard.sweep(ctx)
	return nil
}

// sweep deletes IDs Verify would refuse anyway, so the table stays small
// without the app scheduling anything. Correctness never depends on it,
// so a failure is logged and the call goes ahead.
func (guard *Guard) sweep(ctx context.Context) {
	cutoff := guard.now().Add(-sweepMargin).Unix()
	_, err := guard.db.ExecContext(ctx,
		`DELETE FROM rootcall_used WHERE expires_at < ?`, cutoff)
	if err != nil {
		guard.logger.Error("root call sweep failed", "error", err)
	}
}

func (guard *Guard) refuse(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInternal):
		guard.logger.Error("root call not admitted", "error", err)
		http.Error(response, "internal error",
			http.StatusInternalServerError)
	case errors.Is(err, errTooLarge):
		http.Error(response, "too large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, errBody):
		http.Error(response, "bad request", http.StatusBadRequest)
	default:
		guard.logger.Warn("root call refused", "error", err)
		http.Error(response, "forbidden", http.StatusForbidden)
	}
}
