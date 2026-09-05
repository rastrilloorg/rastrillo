package pow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// A challenge is minted when the form renders and presented back when
// it is submitted. It carries a nonce, the moment it was issued, and
// the difficulty the solver had to meet — all three sealed, so none of
// them can be edited on the way back.
//
// Nothing is written to the database when one is minted. That is
// deliberate, and it is the second design this had: persisting a row
// per challenge made every page view a serialised write against a
// single-writer SQLite file, so a crawler, a link scanner or a
// prefetcher became a denial of service against the one resource every
// submission needs. Nonces are recorded as *spent* instead, on
// acceptance, where each row has already cost a solve.

const (
	// DefaultMinAge is a claim about people, not throughput: nobody
	// reads a form, decides, and writes a sentence about themselves in
	// under three seconds.
	DefaultMinAge = 3 * time.Second

	// DefaultMaxAge bounds how long a minted challenge stays good, and
	// therefore how long a spent nonce has to be remembered.
	DefaultMaxAge = 2 * time.Hour
)

// The form field names. They are unexported because nothing outside
// this package should be writing or reading them by hand: Fields
// renders every one of them and Check reads every one of them, which
// is the only way the two halves cannot drift.
const (
	fieldNonce      = "pow_nonce"
	fieldIssuedAt   = "pow_issued_at"
	fieldDifficulty = "pow_difficulty"
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

// Challenge travels to the browser as hidden fields and comes back the
// same way. Render it with Fields and FormAttrs rather than by hand.
type Challenge struct {
	Nonce      string
	IssuedAt   int64
	Difficulty int
	Seal       string
}

func sealOf(key []byte, nonce string, issuedAt int64, difficulty int) string {
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "%s\x00%d\x00%d", nonce, issuedAt, difficulty)
	return hex.EncodeToString(m.Sum(nil))
}

// newChallenge mints one. The seal is a signature, not encryption:
// nothing in a challenge needs to be secret from the person holding it,
// it only needs to be impossible for them to alter.
func newChallenge(key []byte, now time.Time, difficulty int) Challenge {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand failing is not a condition to degrade through:
		// every anti-abuse property here rests on this nonce being
		// unguessable.
		panic("rastrillo/pow: crypto/rand unavailable")
	}
	nonce := hex.EncodeToString(raw[:])
	issued := now.Unix()
	return Challenge{
		Nonce:      nonce,
		IssuedAt:   issued,
		Difficulty: difficulty,
		Seal:       sealOf(key, nonce, issued, difficulty),
	}
}

// verifySeal reports why a challenge is unacceptable, and whether it is
// acceptable at all.
//
// The order matters: the signature is checked before the timestamp,
// because until the signature holds, IssuedAt is a number the submitter
// chose. A check of an unverified timestamp is not a check.
func verifySeal(key []byte, c Challenge, now time.Time, minAge, maxAge time.Duration) (Reason, bool) {
	want := sealOf(key, c.Nonce, c.IssuedAt, c.Difficulty)
	if !hmac.Equal([]byte(want), []byte(c.Seal)) {
		return ReasonSealInvalid, false
	}
	age := now.Sub(time.Unix(c.IssuedAt, 0))
	switch {
	case age < minAge:
		return ReasonTooFast, false
	case age > maxAge:
		return ReasonTooOld, false
	}
	return "", true
}

// expiry is when a spent nonce stops needing to be remembered: a
// challenge older than maxAge is refused on age alone, so a record
// beyond that point protects nothing.
func (c Challenge) expiry(maxAge time.Duration) time.Time {
	return time.Unix(c.IssuedAt, 0).Add(maxAge)
}

// Fields renders everything the form has to carry: the four sealed
// challenge fields, the empty input the solver writes its counter into,
// and the honeypot.
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
// learned to skip.
func (c Challenge) Fields() template.HTML {
	var b strings.Builder
	hidden := func(name, value string) {
		fmt.Fprintf(&b, "<input type=\"hidden\" name=\"%s\" value=\"%s\">\n",
			name, template.HTMLEscapeString(value))
	}
	hidden(fieldNonce, c.Nonce)
	hidden(fieldIssuedAt, fmt.Sprint(c.IssuedAt))
	hidden(fieldDifficulty, fmt.Sprint(c.Difficulty))
	hidden(fieldSeal, c.Seal)
	fmt.Fprintf(&b, "<input type=\"hidden\" name=\"%s\" value=\"\" data-pow-counter>\n", fieldCounter)
	fmt.Fprintf(&b,
		"<div aria-hidden=\"true\" style=\"position:absolute;left:-9999px;width:1px;height:1px;overflow:hidden\">"+
			"<label for=\"%s\">Leave this field empty</label>"+
			"<input type=\"text\" id=\"%s\" name=\"%s\" tabindex=\"-1\" autocomplete=\"off\"></div>\n",
		fieldHoneypot, fieldHoneypot, fieldHoneypot)
	return template.HTML(b.String())
}

// FormAttrs renders the attributes browser/pow.js looks for on the form
// element:
//
//	<form method="post" {{.Challenge.FormAttrs .WorkerURL}}>
//
// workerURL is the fingerprinted URL of browser/pow-worker.js. The
// module cannot guess it — assets are content-hashed, and the hash
// changes with the bytes — so the template that has the Assets registry
// supplies it.
func (c Challenge) FormAttrs(workerURL string) template.HTMLAttr {
	return template.HTMLAttr(fmt.Sprintf(
		"data-pow-form data-pow-nonce=\"%s\" data-pow-difficulty=\"%d\" data-pow-worker=\"%s\"",
		template.HTMLEscapeString(c.Nonce), c.Difficulty,
		template.HTMLEscapeString(workerURL)))
}
