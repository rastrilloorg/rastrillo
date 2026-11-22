package signin

import (
	"context"

	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/totp"
)

// buildWays builds totp and passkey for the ways that are on. Neither
// gets a Gate: auth.Assembles refuses a SecondFactor hook, so nothing
// is ever held for a second factor here.
func (signins *Signins) buildWays() error {
	if signins.config.Ways&Authenticator != 0 {
		if err := signins.buildCodes(); err != nil {
			return err
		}
	}
	if signins.config.Ways&Passkey != 0 {
		return signins.buildPasskeys()
	}
	return nil
}

func (signins *Signins) buildCodes() error {
	config := signins.config
	codes, err := totp.New(totp.Config{
		DB: config.DB, Sessions: config.Sessions, Key: config.Key[:],
		Issuer: config.Issuer, Logger: config.Logger,
	})
	if err != nil {
		return err
	}
	decoy, err := codes.NewDecoy()
	if err != nil {
		return err
	}
	signins.codes, signins.checkCode = codes, signins.verifyCode
	signins.checkDecoyCode = func(submitted string) {
		if err := codes.CheckDecoy(decoy, submitted); err != nil {
			config.Logger.Error("rastrillo/signin: decoy code", "err", err)
		}
	}
	return nil
}

// verifyCode is totp's Check, plus the decoy's work for a subject with
// no authenticator, whose Check finds no row and returns at once:
// without it, how long a wrong code took would say who had enrolled.
func (signins *Signins) verifyCode(
	_ context.Context, subject, code string,
) (bool, error) {
	enrolled, err := signins.codes.Enrolled(subject)
	if err != nil {
		return false, err
	}
	matched, err := signins.codes.Check(subject, code)
	if !enrolled {
		signins.checkDecoyCode(code)
	}
	return matched, err
}

func (signins *Signins) buildPasskeys() error {
	config := signins.config
	passkeys, err := passkey.New(passkey.Config{
		Sessions: config.Sessions, DB: config.DB, Origin: config.Origin,
		Remember: config.Auth.RememberJar(), Logger: config.Logger,
		Authorize: signins.passkeyAuthorize(),
	})
	if err != nil {
		return err
	}
	signins.passkeys = passkeys
	return nil
}

// passkeyAuthorize is who a passkey may sign in. A passkey skips
// auth.Admit, so without this a holder auth's Authorize would refuse
// still got a session. New has made sure one of the two is set when
// subjects are not addresses.
func (signins *Signins) passkeyAuthorize() func(subject string) bool {
	if signins.config.Authorize != nil {
		return signins.config.Authorize
	}
	return signins.config.Auth.Authorized
}
