// Package totp is an authenticator-app second factor — RFC 6238
// time-based one-time passwords, the six digits every authenticator
// app produces — on the same two seams the passkey package uses: at
// sign-in it completes secondfactor's pending half-session, and at
// step-up it makes a valid-but-stale session fresh again. It never
// signs anybody in from nothing: SignIn demands a live half-session,
// StepUp demands a session.
//
// It is the factor for the person without a passkey-capable device,
// and the one whose proof is a plain form field: every endpoint here
// is a form POST, so a confirm page built on it works with JavaScript
// off — which is also the moment a passkey ceremony cannot run.
//
// The shape: an app builds one *Handlers at boot (New), merges
// totp.Schema into its migrate.Set, registers it with the Gate
// (secondfactor.Gate.Add), and mounts —
//
//	POST /totp/signin   <- form field "code"                (SignIn: pending half-session)
//	POST /totp/stepup   <- form fields "code", "return_to"  (StepUp: a session, stale is fine)
//
// — behind the app's csrf.Protect like every other mutating route.
// Enrolment is the app's pages over Begin, Confirm and Disable: show
// the QR (Enrolment.QR, an inline SVG) and the key, ask for one code
// to prove the app scanned it, and only then is the factor live.
//
// # Secrets at rest
//
// A TOTP secret is symmetric: whoever reads it can mint codes. It is
// sealed under Config.Key (AES-256-GCM, crypto.SealSym) before it
// touches the database, so a copied database file is not a copied
// factor. Derive the key from the instance key (crypto.Derive) — never
// store the key beside the data it seals.
//
// # Guessing
//
// Six digits is a million-code space, and a code stays valid for the
// current 30-second step plus one either side, so a guess is worth
// three in a million. At sign-in the budget is the half-session's
// (secondfactor.Gate.Strike: five misses and the person starts again
// from the first factor); at step-up and enrolment it is a per-subject
// budget of ten misses in fifteen minutes, in memory, the password
// plugin's shape. A verified code's step is remembered and never
// accepted twice, so a code read over a shoulder is spent the moment
// its owner uses it.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Method is the sessions.Session.Method a step-up mints, and the
// suffix a completed sign-in carries ("magiclink+totp").
const Method = "totp"

// The RFC 6238 parameters every authenticator app defaults to. Not
// configurable: an app that deviates loses half its users' apps, and
// the URI advertises them anyway.
const (
	digits = 6
	period = 30 * time.Second
	// skew is how many steps either side of now a code is accepted
	// from: one, for a phone whose clock drifts a little.
	skew = 1
	// secretLen is 160 bits — SHA-1's block, and what RFC 4226 §4
	// recommends as the minimum.
	secretLen = 20
)

// b32 is the alphabet authenticator apps read a key in: RFC 4648,
// uppercase, no padding.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newSecret mints a fresh shared secret.
func newSecret() ([]byte, error) {
	b := make([]byte, secretLen)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// counter is the RFC 6238 time step at t.
func counter(t time.Time) int64 { return t.Unix() / int64(period/time.Second) }

// code is the RFC 4226 HOTP value for one counter: HMAC-SHA1, dynamic
// truncation, six decimal digits, zero-padded.
func code(secret []byte, c int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(c))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", digits, v%1_000_000)
}

// Code is the code an authenticator holding key (the base32 key
// Enrolment shows) produces at t — for a test that wants to play the
// phone. It is also the only way a code is computed here: Verify runs
// the same function over the window.
func Code(key string, t time.Time) (string, error) {
	secret, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(key, " ", "")))
	if err != nil {
		return "", fmt.Errorf("rastrillo/totp: key is not base32: %w", err)
	}
	return code(secret, counter(t)), nil
}

// match checks a submitted code against the window around now and
// returns the step it matched, or -1. Every step is compared in
// constant time whether or not an earlier one matched, so timing
// says nothing about which step hit. minCounter is the last step
// already spent: a step at or below it is not accepted even if the
// digits agree — a code is single use.
func match(secret []byte, submitted string, now time.Time, minCounter int64) int64 {
	submitted = normalize(submitted)
	if len(submitted) != digits {
		return -1
	}
	c := counter(now)
	hit := int64(-1)
	for step := c - skew; step <= c+skew; step++ {
		if subtle.ConstantTimeCompare([]byte(code(secret, step)), []byte(submitted)) == 1 && step > minCounter {
			hit = step
		}
	}
	return hit
}

// normalize strips the spaces and dashes a person types between
// digit groups ("123 456"); everything else must be a digit.
func normalize(code string) string {
	var b strings.Builder
	for _, r := range code {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
		default:
			return ""
		}
	}
	return b.String()
}

// keyURI is the otpauth:// URI authenticator apps scan: label
// "issuer:account", the key, and the parameters spelled out even
// though they are the defaults, because some apps read them.
func keyURI(issuer, account, key string) string {
	q := url.Values{}
	q.Set("secret", key)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(digits))
	q.Set("period", strconv.Itoa(int(period/time.Second)))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}
