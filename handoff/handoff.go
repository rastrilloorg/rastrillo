// Package handoff signs a person in to an instance by way of the root app.
// The instance starts the trip (Enter) and the root later posts a signed
// assertion back to the same path (Consume).
//
// Mount Enter at GET EnterPath and Consume at POST EnterPath, and exempt
// the path from CSRF with csrf.Exempt(handoff.EnterPath). The root's POST
// is cross-site by design, so it carries no same-origin proof; the nonce
// cookie bound to this browser and the root's signature protect it
// instead.
package handoff

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/internal/wire"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// EnterPath is where a handoff starts (GET) and ends (POST).
const EnterPath = "/_carlos/enter"

// Method is the Method of every session a handoff signs in.
const Method = "handoff"

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema holds the table that remembers spent assertion IDs. Apply it with
// migrate.Apply alongside the app's own migrations.
var Schema = migrate.MustFromFS(migrationFS, "handoff")

// ErrConfig is what New returns for a Config it cannot use.
var ErrConfig = errors.New("handoff: configuration")

type Config struct {
	Root      string            // instance.Setup.Root
	Keys      map[string][]byte // instance.Setup.Keys
	Origin    string            // this instance's origin
	Sessions  *sessions.Sessions
	DB        *sql.DB
	MaxAge    time.Duration                       // default time.Hour
	Suspended func(context.Context) (bool, error) // optional
	Now       func() time.Time
	Logger    *slog.Logger

	// RenderRefusal draws the page a refused handoff shows, so the app
	// can brand it. It must answer with http.StatusForbidden; the
	// Cache-Control header is already set. Nil gets a plain page.
	RenderRefusal func(http.ResponseWriter, *http.Request, RefusalPage)

	// SubjectFor, if set, turns the root's subject into the one this
	// app's sessions carry, such as a row ID that sessions.UserID can
	// parse. Consume calls it once every check has passed and before it
	// spends the assertion, so an error or an empty subject refuses the
	// handoff and leaves the assertion and the nonce cookie for a retry.
	// A spent assertion is refused before SubjectFor runs, but two first
	// uses can race and both call it, so it must be idempotent. Nil keeps
	// the root's subject.
	SubjectFor func(ctx context.Context, rootSubject string) (string, error)
}

// RefusalPage is what a refused handoff tells the person. LinkText and
// LinkURL are empty when there is nowhere useful to send them.
type RefusalPage struct {
	Title    string
	Message  string
	LinkText string
	LinkURL  string
}

type Handoff struct {
	root          string
	origin        string
	verifier      *assertion.Verifier
	sessions      *sessions.Sessions
	db            *sql.DB
	maxAge        time.Duration
	suspended     func(context.Context) (bool, error)
	now           func() time.Time
	logger        *slog.Logger
	renderRefusal func(http.ResponseWriter, *http.Request, RefusalPage)
	subjectFor    func(context.Context, string) (string, error)
}

func New(config Config) (*Handoff, error) {
	if config.Sessions == nil || config.DB == nil ||
		!wire.Origin(config.Root) || !wire.Origin(config.Origin) {
		return nil, ErrConfig
	}
	// Signed mode is a path tenant, which uses the hub's session and
	// never a handoff.
	if config.Sessions.Signed() {
		return nil, ErrConfig
	}
	verifier, err := assertion.NewVerifier(
		config.Root, config.Origin, config.Keys)
	if err != nil {
		return nil, ErrConfig
	}
	handoff := &Handoff{
		root:          config.Root,
		origin:        config.Origin,
		verifier:      verifier,
		sessions:      config.Sessions,
		db:            config.DB,
		maxAge:        config.MaxAge,
		suspended:     config.Suspended,
		now:           config.Now,
		logger:        config.Logger,
		renderRefusal: config.RenderRefusal,
		subjectFor:    config.SubjectFor,
	}
	if handoff.renderRefusal == nil {
		handoff.renderRefusal = renderDefaultRefusal
	}
	if handoff.maxAge <= 0 {
		handoff.maxAge = time.Hour
	}
	if handoff.now == nil {
		handoff.now = time.Now
	}
	if handoff.logger == nil {
		handoff.logger = slog.Default()
	}
	return handoff, nil
}
