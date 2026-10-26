package oidc

import (
	"fmt"
	"regexp"
	"strings"
)

// The two identity providers this package supports, and what each needs
// an app to configure. Ported from ips's providers.go.
//
// It is a closed list rather than a free-text issuer box, and that stays
// true in this port for the same reason ips gives: an issuer field would
// let an app point this package at anything answering an OpenID discovery
// document, which is a wider trust decision than "we use Microsoft" or
// "we use Google" and is not one a package shipped to many apps should
// make on an app's behalf. A third provider (Okta, named in the discussion
// this design answers but not built or interop-tested anywhere this
// package can cite) is a row in this list plus the tests beside it, added
// once somebody can run it against a real tenant — see the design's D2.

// Kind identifies a provider. If this ever backs a stored row, these
// spellings must not change once shipped.
type Kind string

const (
	// Microsoft is Entra ID — Azure AD as was.
	Microsoft Kind = "microsoft"
	// Google is Google Workspace.
	Google Kind = "google"
)

// Provider is everything that differs between one provider and the next,
// in one place, so a caller and a sign-in cannot come to disagree about
// what a connection means.
type Provider struct {
	Kind Kind

	// Label is what a person calls it.
	Label string

	// Directory is the label for the one value an app must supply beyond
	// the client id and secret, or "" for a provider that needs none.
	// Google's issuer is the same for every customer; Microsoft has one
	// per tenant, and that tenant IS the trust boundary.
	Directory string

	// DirectoryHint says where to find it.
	DirectoryHint string

	// NeedsHostedDomain marks a provider whose ISSUER is shared by every
	// customer it has, so nothing about the signature says which
	// organisation a token came from. Google is the case: its issuer is
	// https://accounts.google.com for a Workspace tenant and for a
	// personal Gmail account alike. What closes it is the `hd` claim —
	// the Workspace domain Google itself asserts — checked by this
	// package against Connection.AllowedDomains, which is REQUIRED for
	// such a provider (see the design's D3).
	NeedsHostedDomain bool

	// RedirectHint is the exact thing to paste into the provider's own
	// console. Getting this wrong is the commonest way a connection
	// fails, and it fails with the provider's own error rather than
	// this package's, so it is printed rather than merely described.
	RedirectHint string

	// issuer builds the issuer identifier from the directory value.
	issuer func(directory string) string

	// refuse says why a directory value will not be accepted, or "".
	refuse func(directory string) string
}

// providers is the list. Order is the order a sign-in screen should offer
// them in.
var providers = []Provider{
	{
		Kind:      Microsoft,
		Label:     "Microsoft",
		Directory: "Directory (tenant) ID",
		DirectoryHint: "From the Microsoft Entra admin centre, on the app registration's " +
			"overview page. The identifier itself, not your domain.",
		RedirectHint: "Add it to the registration as a Web redirect URI.",
		issuer: func(directory string) string {
			return "https://login.microsoftonline.com/" + strings.TrimSpace(directory) + "/v2.0"
		},
		refuse: refuseMicrosoftDirectory,
	},
	{
		Kind:              Google,
		Label:             "Google",
		NeedsHostedDomain: true,
		RedirectHint:      "Add it to the OAuth client as an authorised redirect URI.",
		issuer:            func(string) string { return "https://accounts.google.com" },
		refuse:            func(string) string { return "" },
	},
}

// Providers is the list, for a caller that offers a choice.
func Providers() []Provider {
	out := make([]Provider, len(providers))
	copy(out, providers)
	return out
}

// ProviderFor looks one up. An unknown kind is refused rather than
// defaulted: a caller naming a provider this package does not have is a
// caller nothing should act on, and picking the first one would silently
// sign people in against a directory nobody chose.
func ProviderFor(kind Kind) (Provider, bool) {
	for _, p := range providers {
		if p.Kind == kind {
			return p, true
		}
	}
	return Provider{}, false
}

// NeedsDirectory reports whether this provider asks for a directory value
// at all.
func (p Provider) NeedsDirectory() bool { return p.Directory != "" }

// Issuer is the issuer identifier for one connection's directory value, or
// a refusal naming what is wrong with it.
func (p Provider) Issuer(directory string) (string, error) {
	if why := p.Refusal(directory); why != "" {
		return "", fmt.Errorf("%w: %s", ErrNotConfigured, why)
	}
	return p.issuer(directory), nil
}

// Refusal is why this directory value will not be accepted, or "". Call
// it before saving a connection, so an app is told at the point of typing
// rather than at somebody's first sign-in.
func (p Provider) Refusal(directory string) string {
	directory = strings.TrimSpace(directory)
	if !p.NeedsDirectory() {
		return ""
	}
	if directory == "" {
		return "This is needed: it is what says which organisation's directory may sign people in."
	}
	if p.refuse == nil {
		return ""
	}
	return p.refuse(directory)
}

// guid matches a directory id, and domainish matches a tenant's own
// domain. Both are accepted because both work, and an app reads whichever
// its console happened to show it.
var (
	guid      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	domainish = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)
)

// sharedTenants are the endpoints that sign for every organisation rather
// than for one.
//
// They are refused BY NAME here as well as by Discover's placeholder check
// later. Both refusals are kept, deliberately: the placeholder check is
// the one that cannot be got round, because it reads what the provider
// actually published, but it fires at sign-in — where the person reading
// the message is not the person who configured the connection. This one
// fires while somebody is still typing.
//
// What accepting one would cost: an ID token from `/common` is signed by
// Microsoft for whichever tenant the person happens to be in, so every
// Entra account in the world would reach this connection's admission
// gate. The tenant is not a setting, it is the boundary.
var sharedTenants = []string{"common", "organizations", "consumers"}

func refuseMicrosoftDirectory(directory string) string {
	directory = strings.TrimSpace(directory)
	for _, shared := range sharedTenants {
		if strings.EqualFold(directory, shared) {
			return "That is Microsoft's shared endpoint rather than your own directory, and it " +
				"would let an account from any organisation reach this sign-in. Use the " +
				"directory (tenant) ID from your own Entra admin centre."
		}
	}
	if guid.MatchString(directory) {
		return ""
	}
	// A DOMAIN IS REFUSED, and ips's first draft accepted one. Microsoft
	// serves a discovery document at a domain's own path, so it looks
	// like it works — but the document canonicalises `issuer` to the
	// tenant's GUID, and every ID token carries the GUID too. Discover
	// compares the two and refuses the mismatch, so a connection
	// configured by domain would save cleanly, turn on cleanly, and then
	// refuse everybody at the first step with a message about issuers
	// that reads as a fault at Microsoft.
	if domainish.MatchString(directory) {
		return "That looks like a domain. Microsoft identifies a directory by its ID — the " +
			"Directory (tenant) ID on the app registration's overview page — and a token " +
			"signed for your tenant names that ID rather than your domain."
	}
	return "This should be the directory ID — eight characters, then three groups of four, " +
		"then twelve, separated by hyphens."
}
