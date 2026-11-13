package auth

import (
	"context"
	"strings"
	"time"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/mail"
)

type BlindLink struct {
	Hash      string
	LookupKey string
	Purpose   string
	Expires   time.Time
}

type BlindLinkStore interface {
	Put(context.Context, BlindLink) error
	Take(context.Context, string) (BlindLink, bool, error)
	Spend(context.Context, string) (int, error)
	Sweep(context.Context, time.Time) error
}

type BlindLinks struct {
	Store          BlindLinkStore
	LookupKey      func(context.Context, string) (string, error)
	ResolveSubject func(context.Context, string) (string, error)
}

var ErrBlindConfiguration = signin.ErrLinkInvalid
var ErrBlindOperation = signin.ErrLinkInvalid

func blindConfig(cfg Config) error {
	blind := cfg.BlindLinks
	if blind == nil {
		return nil
	}
	_, logs := cfg.Mailer.(*signin.LogMailer)
	switch {
	case logs:
		return ErrBlindConfiguration
	case blind.Store == nil || blind.LookupKey == nil ||
		blind.ResolveSubject == nil:
		return ErrBlindConfiguration
	case cfg.Records != nil || cfg.Blind || cfg.Pepper != nil ||
		cfg.Signed != nil:
		return ErrBlindConfiguration
	case cfg.Authorize != nil || cfg.SubjectFor != nil || cfg.SigninScreen:
		return ErrBlindConfiguration
	case len(cfg.KeymailServers) != 0:
		return ErrBlindConfiguration
	case cfg.Mailer == nil || !mail.IsConfigured(cfg.Mailer):
		return ErrBlindConfiguration
	}
	return nil
}

type blindLinkAdapter struct {
	config BlindLinks
	now    func() time.Time
}

func opaque(value string) bool {
	return value != "" && strings.TrimSpace(value) == value &&
		!strings.Contains(value, "@")
}

func (blind *BlindLinks) lookup(ctx context.Context, address string) (
	string, error,
) {
	key, err := blind.LookupKey(ctx,
		strings.ToLower(strings.TrimSpace(address)))
	if err != nil || !opaque(key) {
		return "", ErrBlindOperation
	}
	return key, nil
}

func (store *blindLinkAdapter) PutLink(ctx context.Context,
	hash, address, purpose string, expires time.Time,
) error {
	key, err := store.config.lookup(ctx, address)
	if err != nil {
		return err
	}
	return blindError(store.config.Store.Put(ctx, BlindLink{
		Hash: hash, LookupKey: key, Purpose: purpose, Expires: expires,
	}))
}

func (store *blindLinkAdapter) TakeLink(ctx context.Context,
	hash, purpose string,
) (string, bool, error) {
	link, ok, err := store.config.Store.Take(ctx, hash)
	if err != nil {
		return "", false, ErrBlindOperation
	}
	if !ok || link.Hash != hash || link.Purpose != purpose ||
		!link.Expires.After(store.now()) || !opaque(link.LookupKey) {
		return "", false, nil
	}
	return link.LookupKey, true, nil
}

func blindError(err error) error {
	if err != nil {
		return ErrBlindOperation
	}
	return nil
}

func (auth *Auth) wireBlindLinks(links *BlindLinks) {
	auth.flow.Links = &blindLinkAdapter{
		config: *links, now: func() time.Time { return auth.now() },
	}
	auth.flow.Classifier = nil
	auth.flow.Keymail = nil
	auth.flow.Now = func() time.Time { return auth.now() }
	auth.flow.Limiter = &blindLimiter{
		next: auth.flow.Limiter,
		pepper: crypto.Derive([]byte(auth.cfg.InstanceKey),
			"rastrillo/auth/blind-limiter/v1"),
	}
}

type blindLimiter struct {
	next   signin.Limiter
	pepper []byte
}

func (limiter *blindLimiter) Allow(ctx context.Context, key string,
	now time.Time, budget int,
) (bool, error) {
	return limiter.next.Allow(ctx, emailHash(limiter.pepper, key), now, budget)
}

func (auth *Auth) blindSubject(
	ctx context.Context, key string,
) (string, error) {
	subject, err := auth.cfg.BlindLinks.ResolveSubject(ctx, key)
	if err != nil || !opaque(subject) {
		return "", ErrBlindOperation
	}
	return subject, nil
}

func (auth *Auth) spendBlindLinks(ctx context.Context, address string) (
	int, error,
) {
	key, err := auth.cfg.BlindLinks.lookup(ctx, address)
	if err != nil {
		return 0, err
	}
	count, err := auth.cfg.BlindLinks.Store.Spend(ctx, key)
	return count, blindError(err)
}
