// Package oidc is a relying party for OpenID Connect: enough of the
// authorization-code flow to let somebody prove, to an identity provider
// an app's operator already trusts, that they are who they say they are —
// and no more than that.
//
// Ported from ips (a Rastrillo app), whose internal/oidc was ~300 lines of
// the standard library checking a single RS256 signature itself rather
// than importing a general JOSE stack — docs/superpowers/specs/2026-10-26-
// oidc-signin-design.md has the full design and the review that shaped it.
// The verification core (this file, verify.go, keys.go, providers.go) holds
// no database handle and knows nothing about any particular app; handlers.go
// is the HTTP wiring that does.
package oidc

import "time"

// Identity is what a verified ID token said. Every field here was inside
// the signature; nothing was read from a query string, a redirect, or an
// unauthenticated response.
type Identity struct {
	// Subject is the provider's own permanent identifier for this person
	// within this tenant (the token's `sub`). It is RECORDED and does not
	// by itself resolve an app's own account — see Config.Admit's doc
	// comment for why the app's own subject is a separate decision.
	Subject string

	// Issuer is the issuer that signed the token, as verified.
	Issuer string

	// Address is the email address the token carried. It may be empty: a
	// provider is not obliged to release one, and a caller must treat
	// that as a refusal rather than as an empty match. See address() in
	// verify.go for exactly which claim this comes from and why.
	Address string

	// AddressVerified records what the provider SAID about the address,
	// and the three states are kept apart on purpose: Google asserts the
	// claim, Microsoft Entra never emits it at all, and folding silence
	// into "false" would refuse every Entra sign-in while folding it into
	// "true" asserts something nobody said.
	AddressVerified Tristate

	// Name is the display name, where one was released. Used to describe
	// a sign-in in a register and for nothing else — never to decide
	// anything.
	Name string

	// HostedDomain is Google's `hd` — the Workspace domain an account
	// belongs to, absent for a personal account and for a provider that
	// does not issue one. For a provider whose issuer is shared by every
	// customer it has, this is the ONLY thing in a signed token that says
	// which organisation it came from; see Provider.NeedsHostedDomain.
	HostedDomain string

	// Tenant is Entra's `tid`, empty for a provider that does not issue
	// one. Recorded rather than acted on: the tenant binding is enforced
	// by the issuer comparison in Discover and Verify, a check on the
	// signature's own subject rather than on a claim inside it.
	Tenant string

	// Methods is the `amr` claim — what the provider says it actually did
	// to authenticate this person. It is recorded and decides nothing:
	// this application's own second factor runs regardless. See the
	// design's D6 for why a claim asserting multi-factor is never
	// accepted in place of running one.
	Methods []string

	// AuthTime is the provider's `auth_time` where it gave one, and the
	// zero time otherwise. This package does not substitute the current
	// time for a figure the provider did not state; a caller minting a
	// session in Signed/hub mode must clamp this to its own clock before
	// using it — see the design's D5.
	AuthTime time.Time
}

// Tristate is "yes", "no" and "the provider did not say", which are three
// different facts about a claim, folded into two only by somebody who has
// decided which way silence should go.
type Tristate int

const (
	// Unstated means the claim was absent. It is the zero value, so a
	// caller that forgets to look at this field is looking at the honest
	// answer rather than at an assertion.
	Unstated Tristate = iota
	// Asserted means the provider said the address is verified.
	Asserted
	// Denied means the provider said it is not.
	Denied
)

func (t Tristate) String() string {
	switch t {
	case Asserted:
		return "verified by the provider"
	case Denied:
		return "not verified by the provider"
	default:
		return "the provider did not say"
	}
}
