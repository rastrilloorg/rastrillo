package signin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"amadan.net/rastrillo/rastrillo/password"
)

// passphrases keeps one passphrase hash per subject. Its own table
// rather than the password plugin's, whose accounts are the app's
// int64 ids: auth's subjects are addresses or SubjectFor's values, and
// two tables keyed differently would make two people of one.
type passphrases struct {
	database *sql.DB
	now      func() time.Time
}

// execer is the database or a transaction on it, so a write can stand
// alone or join others.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (
		sql.Result, error)
}

// set replaces subject's passphrase.
func (store passphrases) set(
	ctx context.Context, subject, passphrase string,
) error {
	hash, err := hashFor(subject, passphrase)
	if err != nil {
		return err
	}
	return store.put(ctx, store.database, subject, hash)
}

// hashFor is the hash to keep for subject's passphrase. The length rules
// belong to the page that asks for it; this only refuses to key a row by
// nothing.
func hashFor(subject, passphrase string) (string, error) {
	if subject == "" {
		return "", errors.New("rastrillo/signin: a passphrase needs a subject")
	}
	return password.Hash(passphrase)
}

func (store passphrases) put(
	ctx context.Context, database execer, subject, hash string,
) error {
	_, err := database.ExecContext(ctx,
		`INSERT INTO signin_passphrases (subject, hash, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT (subject) DO UPDATE
		 SET hash = excluded.hash, created_at = excluded.created_at`,
		subject, hash, store.now().UTC().Format(time.RFC3339))
	return err
}

// keepPassphrase stores subject's passphrase and answers the offer in
// one transaction, so neither is kept without the other and a failure
// leaves any passphrase they had. It hashes first, so the write lock is
// not held through Argon2.
func (signins *Signins) keepPassphrase(
	ctx context.Context, subject, passphrase string,
) error {
	hash, err := hashFor(subject, passphrase)
	if err != nil {
		return err
	}
	transaction, err := signins.config.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	err = signins.passphrases.put(ctx, transaction, subject, hash)
	if err == nil {
		err = signins.dismissIn(ctx, transaction, subject)
	}
	if err != nil {
		return err
	}
	return transaction.Commit()
}

// remove forgets subject's passphrase; none is not an error.
func (store passphrases) remove(
	ctx context.Context, subject string,
) error {
	_, err := store.database.ExecContext(ctx,
		`DELETE FROM signin_passphrases WHERE subject = ?`, subject)
	return err
}

// hash reports a missing row as not found rather than an error, since
// an unknown address is the ordinary case and must run the decoy.
func (store passphrases) hash(
	ctx context.Context, subject string,
) (string, bool, error) {
	var hash string
	err := store.database.QueryRowContext(ctx,
		`SELECT hash FROM signin_passphrases WHERE subject = ?`,
		subject).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return hash, err == nil, err
}

// verifyPassphrase is whether submitted is subject's passphrase. A
// subject with none is verified against the decoy all the same, so an
// unknown address costs what a wrong passphrase does.
func (signins *Signins) verifyPassphrase(
	ctx context.Context, subject, submitted string,
) (bool, error) {
	hash, found, err := signins.passphrases.hash(ctx, subject)
	if err != nil {
		return false, err
	}
	if !found {
		password.Verify(signins.decoy, submitted)
		return false, nil
	}
	return password.Verify(hash, submitted), nil
}

// decoyHash is a hash of random bytes nobody knows, made once per
// process at the parameters password.Hash uses today.
func decoyHash() (string, error) {
	return password.Hash(rand.Text())
}
