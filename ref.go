package rastrillo

import (
	"crypto/rand"
	"encoding/base32"
	"net/http"
)

// refEncoding is lowercase RFC 4648 base32 with no padding: the digits
// 0, 1, 8 and 9 are absent by construction, so a reference read aloud
// down a phone line cannot be confused with an O, l or B. Lowercase
// because it is displayed inside a sentence, not shouted.
var refEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewRef mints the short reference an error page shows and the log line
// carries: six lowercase base32 characters over 4 bytes of crypto/rand —
// 30 bits, enough that two errors in the same log window will not share
// one, and short enough that a person will actually quote it.
//
// It is not a secret and not an id: nothing is stored under it. Its only
// job is to join what the user saw to what the operator grepped, which
// is why it appears in exactly two places — the page and the log.
func NewRef() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return refEncoding.EncodeToString(b[:])[:6]
}

// ErrorPageFunc renders an error response body: the app's own page, in
// its own shell, for a status the framework or a generated action
// reached rather than the app. ref is the NewRef the failure was logged
// under, empty for the statuses that have nothing to reference (404,
// 403). It is the type of both Ctx.ErrorPage and Options.ErrorPage —
// one shape, so an app writes the function once and wires it to both.
//
// The callback owns the status code as well as the body: it must call
// WriteHeader(status) itself.
type ErrorPageFunc func(w http.ResponseWriter, r *http.Request, status int, ref string)
