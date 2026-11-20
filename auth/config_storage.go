package auth

import (
	"bytes"
	"errors"

	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/mail"
)

func configureStorage(cfg *Config) error {
	if cfg.SigninOneWay && !cfg.SigninScreen {
		return errors.New(
			"rastrillo/auth: Config.SigninOneWay needs SigninScreen")
	}
	if err := blindConfig(*cfg); err != nil {
		return err
	}
	// Freeze validated hooks so caller edits cannot change a live flow.
	if cfg.BlindLinks != nil {
		blind := *cfg.BlindLinks
		cfg.BlindLinks = &blind
	}
	if cfg.Records != nil {
		if err := recordsConfig(*cfg); err != nil {
			return err
		}
		cfg.Pepper = bytes.Clone(cfg.Pepper)
	} else {
		// Half a mode would start: a pepper nothing uses, or a blind flag
		// over a database that stores the address anyway.
		if cfg.Pepper != nil || cfg.Blind || cfg.Signed != nil {
			return errors.New(
				"rastrillo/auth: Config.Pepper, Blind and Signed are " +
					"records mode and need Config.Records",
			)
		}
		if cfg.DB == nil {
			return errors.New("rastrillo/auth: Config.DB is required")
		}
	}
	return nil
}

// recordsConfig refuses a records-mode Config that could not work as
// a centre.
func recordsConfig(cfg Config) error {
	switch {
	case cfg.DB != nil:
		return errors.New(
			"rastrillo/auth: Config.Records and Config.DB are two " +
				"modes; set one",
		)
	case cfg.Signed == nil || cfg.Signed.Key == nil:
		return errors.New(
			"rastrillo/auth: Config.Records needs Config.Signed with a " +
				"Key: the centre mints the session cookie",
		)
	case cfg.Signed.Epoch != nil:
		return errors.New(
			"rastrillo/auth: leave Config.Signed.Epoch nil with " +
				"Records: auth reads the epoch from users/",
		)
	case len(cfg.Pepper) < 32:
		return errors.New(
			"rastrillo/auth: Config.Pepper must be at least 32 bytes",
		)
	case cfg.SubjectFor != nil:
		return errors.New(
			"rastrillo/auth: Config.SubjectFor does not apply with " +
				"Records: the subject is the user id",
		)
	case cfg.Blind && (cfg.Mailer == nil || !mail.IsConfigured(cfg.Mailer)):
		// The nil fallback and mail.Logged (what mail.FromEnv returns
		// with no relay) both write the recipient and the live link to
		// the log, which is what blind mode keeps the address out of.
		return errors.New(
			"rastrillo/auth: Config.Blind needs a Mailer that delivers; " +
				"mail.Logged writes the address to the log. For local " +
				"development, pass a Sender of your own",
		)
	}
	return nil
}

// Without the shipped screen, remembered addresses serve no UI and must
// not be written. Remember only affects apps that enable the screen.
func rememberMode(cfg Config) lastsignin.Mode {
	switch {
	case !cfg.SigninScreen:
		return lastsignin.Off
	case cfg.Remember != nil && !*cfg.Remember:
		return lastsignin.Forgetting
	default:
		return lastsignin.On
	}
}
