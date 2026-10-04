package pow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A challenge is minted when a form renders and presented back when it
// is submitted. Nothing is written when one is minted: a row per render
// made every page view a serialised write against a single-writer
// SQLite file, so a crawler or a prefetcher was a denial of service
// against the one resource every submission needs. Nonces are recorded
// as spent on acceptance instead, where each row has already cost a
// solve.

const (
	// DefaultMinAge is a claim about people, not throughput: nobody
	// reads a form, decides, and writes a sentence about themselves in
	// under three seconds.
	DefaultMinAge = 3 * time.Second

	// DefaultMaxAge bounds how long a minted challenge stays good, and
	// therefore how long a spent nonce has to be remembered.
	DefaultMaxAge = 2 * time.Hour
)

// The form field names. Unexported because Fields renders every one and
// readChallenge reads every one, which is the only way the halves
// cannot drift.
const (
	fieldScope      = "pow_scope"
	fieldNonce      = "pow_nonce"
	fieldIssued     = "pow_issued"  // milliseconds
	fieldExpires    = "pow_expires" // milliseconds
	fieldDifficulty = "pow_difficulty"
	fieldFlags      = "pow_flags"
	fieldSeal       = "pow_seal"
	fieldCounter    = "pow_counter"

	// fieldHoneypot is a real input, hidden off-screen, that a person
	// never fills in. The name is chosen to be invisible to browser
	// autofill and password managers: anything resembling "company",
	// "organisation" or "url" gets filled for real people, and a filled
	// honeypot silently discards a genuine submission behind a cheerful
	// success page.
	fieldHoneypot = "hp"
)

const (
	// flagTrapOmitted marks a recovery challenge: rendered without the
	// honeypot, so a password manager that filled it cannot fill it
	// again, and Admit skips the honeypot for this token only.
	flagTrapOmitted uint8 = 1 << 0
	// flagBound marks work bound to a submitted value. Sealed so a bound
	// challenge cannot be presented as unbound and verified without the
	// value it was bound to.
	flagBound uint8 = 1 << 1
)

// maxScopeLen bounds the one free-text sealed field. Scopes are short
// names chosen by the server; anything longer was sent by somebody
// probing, and is refused before it is hashed.
const maxScopeLen = 256

// maxDifficulty bounds the posted difficulty before it is used: SHA-256
// has 256 bits, and an int32 in the seal must not be fed a value that
// wraps.
const maxDifficulty = 256

// Challenge travels to the browser as hidden fields and back the same
// way. Every field is sealed; the seal is a signature, not encryption.
type Challenge struct {
	Scope      string
	Nonce      string
	Issued     time.Time // millisecond precision, the precision it is sealed at
	Expires    time.Time // absolute; fixed at issue so a later MaxAge cannot extend it
	Difficulty int       // 0 for a NoProof guard
	Flags      uint8
	Seal       string
}

// sealInput is the exact byte string the HMAC covers. Every variable
// length field is length-prefixed and every number fixed width, so no
// two different challenges share an input.
func sealInput(c Challenge) []byte {
	b := []byte("pow/v2")
	b = binary.AppendUvarint(b, uint64(len(c.Scope)))
	b = append(b, c.Scope...)
	b = binary.AppendUvarint(b, uint64(len(c.Nonce)))
	b = append(b, c.Nonce...)
	b = binary.BigEndian.AppendUint64(b, uint64(c.Issued.UnixMilli()))
	b = binary.BigEndian.AppendUint64(b, uint64(c.Expires.UnixMilli()))
	b = binary.BigEndian.AppendUint32(b, uint32(int32(c.Difficulty)))
	return append(b, c.Flags)
}

func sealOf(key []byte, c Challenge) string {
	m := hmac.New(sha256.New, key)
	m.Write(sealInput(c))
	return hex.EncodeToString(m.Sum(nil))
}

func sealOK(key []byte, c Challenge) bool {
	return hmac.Equal([]byte(sealOf(key, c)), []byte(c.Seal))
}

// validNonce is exactly what newChallenge mints. Checked before the
// HMAC, so the nonce is also a canonical identifier: one token, one
// string, and nothing else reaches the hash or the database.
func validNonce(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func newChallenge(key []byte, issued time.Time, maxAge time.Duration, scope string, difficulty int, flags uint8) Challenge {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Every anti-abuse property here rests on the nonce being
		// unguessable; there is no degraded mode to fall back to.
		panic("rastrillo/pow: crypto/rand unavailable")
	}
	at := time.UnixMilli(issued.UnixMilli())
	c := Challenge{
		Scope:      scope,
		Nonce:      hex.EncodeToString(raw[:]),
		Issued:     at,
		Expires:    at.Add(maxAge),
		Difficulty: difficulty,
		Flags:      flags,
	}
	c.Seal = sealOf(key, c)
	return c
}

// readChallenge parses the posted fields. No seal and no nonce is
// ReasonMissing: the form was never wired, which an operator needs to
// tell apart from an attack. Anything present but malformed is
// ReasonSealInvalid, one reason for every shape, so a prober learns
// nothing from the difference.
func readChallenge(r *http.Request) (Challenge, Reason) {
	f := r.PostForm
	nonce, seal := f.Get(fieldNonce), f.Get(fieldSeal)
	if nonce == "" && seal == "" {
		return Challenge{}, ReasonMissing
	}
	scope := f.Get(fieldScope)
	if !validNonce(nonce) || len(seal) != 2*sha256.Size || len(scope) > maxScopeLen {
		return Challenge{}, ReasonSealInvalid
	}
	issued, err1 := strconv.ParseInt(f.Get(fieldIssued), 10, 64)
	expires, err2 := strconv.ParseInt(f.Get(fieldExpires), 10, 64)
	diff, err3 := strconv.Atoi(f.Get(fieldDifficulty))
	flags, err4 := strconv.ParseUint(f.Get(fieldFlags), 10, 8)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || diff < 0 || diff > maxDifficulty {
		return Challenge{}, ReasonSealInvalid
	}
	return Challenge{
		Scope: scope, Nonce: nonce,
		Issued: time.UnixMilli(issued), Expires: time.UnixMilli(expires),
		Difficulty: diff, Flags: uint8(flags), Seal: seal,
	}, ""
}

// Fields renders everything the form has to carry: every sealed
// challenge field, the empty input the solver writes its counter into,
// and the honeypot, unless this is a recovery challenge.
//
// One call rather than a documented list of inputs, because each piece
// has a way of being subtly wrong. The honeypot most of all: it needs
// aria-hidden on its wrapper, tabindex="-1" so nothing focusable hides
// behind that, autocomplete="off", and a name no browser will autofill.
// Miss one and a screen-reader user, or anyone with a password manager,
// fills the trap and has a real submission silently discarded behind a
// cheerful success page.
//
// It is positioned off-screen with an inline style rather than a class,
// so there is no stylesheet to vendor and nothing for an app's CSS to
// fail to carry. Not display:none: a hidden input is a shape some bots
// learned to skip. The style is fixed so that a CSP can admit it by
// hash — see HoneypotStyleHash.
func (c Challenge) Fields() template.HTML {
	var b strings.Builder
	hidden := func(name, value string) {
		fmt.Fprintf(&b, "<input type=\"hidden\" name=\"%s\" value=\"%s\">\n",
			name, template.HTMLEscapeString(value))
	}
	hidden(fieldScope, c.Scope)
	hidden(fieldNonce, c.Nonce)
	hidden(fieldIssued, strconv.FormatInt(c.Issued.UnixMilli(), 10))
	hidden(fieldExpires, strconv.FormatInt(c.Expires.UnixMilli(), 10))
	hidden(fieldDifficulty, strconv.Itoa(c.Difficulty))
	hidden(fieldFlags, strconv.Itoa(int(c.Flags)))
	hidden(fieldSeal, c.Seal)
	fmt.Fprintf(&b, "<input type=\"hidden\" name=\"%s\" value=\"\" data-pow-counter>\n", fieldCounter)
	if c.Flags&flagTrapOmitted == 0 {
		fmt.Fprintf(&b,
			"<div aria-hidden=\"true\" style=\"%s\">"+
				"<label for=\"%s\">Leave this field empty</label>"+
				"<input type=\"text\" id=\"%s\" name=\"%s\" tabindex=\"-1\" autocomplete=\"off\"></div>\n",
			honeypotStyle, fieldHoneypot, fieldHoneypot, fieldHoneypot)
	}
	return template.HTML(b.String())
}

// honeypotStyle is the honeypot wrapper's inline style. Changing one
// byte of it without recomputing HoneypotStyleHash makes a strict CSP
// block it, and the trap renders in plain view.
const honeypotStyle = "position:absolute;left:-9999px;width:1px;height:1px;overflow:hidden"

// HoneypotStyleHash is the CSP hash source that admits the honeypot's
// inline style, for a policy whose style-src has no 'unsafe-inline'.
// A hash matches a style attribute only alongside 'unsafe-hashes':
//
//	style-src 'self' 'unsafe-hashes' 'sha256-…'
//
// rastrillo's default policy already carries both. An app that replaces
// the policy through Options.CSP must restate them, or its public forms
// show the trap field.
const HoneypotStyleHash = "'sha256-yJxAE4rjdcckohdlnvecSporPcqS9xOaA4hJxi87LMc='"
