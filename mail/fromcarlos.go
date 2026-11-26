package mail

import (
	"fmt"
	"log/slog"
	netmail "net/mail"
	"os"
	"strings"
)

// carlosMailEnv is set to "submit" by the CARLOS agent, and only by it,
// when the app's mail goes through the platform's verb. The agent sets
// it only once the control socket is bound, and strips a copy the app's
// own environment carries.
const carlosMailEnv = "CARLOS_MAIL"

// FromCarlos builds the Sender for an app that must send real mail, and
// is an error when it cannot. Call it where the app refuses to start
// without mail, and stop if it fails.
//
// With $CARLOS_MAIL=submit it returns Submit, From <prefix>_MAIL_FROM.
// Otherwise it returns SMTP from <prefix>_SMTP_*, exactly as FromEnv
// does. With neither, there is no Logged fallback: a sign-in link that
// is only logged is not a sign-in. Apps that allow logged mail keep
// FromEnv.
func FromCarlos(prefix string, logger *slog.Logger) (Sender, error) {
	if logger == nil {
		logger = slog.Default()
	}
	switch mode := os.Getenv(carlosMailEnv); mode {
	case "submit":
		return submitFromEnv(prefix, logger)
	case "":
	default:
		logger.Warn("rastrillo/mail: $CARLOS_MAIL is not \"submit\"; "+
			"using SMTP", "CARLOS_MAIL", mode)
	}
	host := strings.TrimSpace(os.Getenv(prefix + "_SMTP_HOST"))
	from := strings.TrimSpace(os.Getenv(prefix + "_SMTP_FROM"))
	if host == "" || from == "" {
		return nil, fmt.Errorf("rastrillo/mail: neither CARLOS_MAIL=submit "+
			"nor %s_SMTP_HOST and %s_SMTP_FROM is set, so this app cannot "+
			"send mail; on CARLOS run `carlos email enable`, elsewhere "+
			"set the %s_SMTP_* variables", prefix, prefix, prefix)
	}
	return FromEnv(prefix, logger), nil
}

// carlosSubmit is FromEnv's platform mail: the submit Sender when
// CARLOS_MAIL=submit and it is fully configured, else nil. Half
// configured is warned about, and FromEnv falls back to SMTP or the log
// rather than refusing to start, which is FromCarlos's job.
func carlosSubmit(prefix string, logger *slog.Logger) Sender {
	if os.Getenv(carlosMailEnv) != "submit" {
		return nil
	}
	sender, err := submitFromEnv(prefix, logger)
	if err != nil {
		logger.Warn("rastrillo/mail: CARLOS mail is on but cannot be used; "+
			"falling back to SMTP or the log", "err", err)
		return nil
	}
	return sender
}

// submitFromEnv is FromCarlos under CARLOS_MAIL=submit. Each missing
// variable is a refusal here rather than a failed send later, because
// the first send is often a person waiting for a sign-in link.
func submitFromEnv(prefix string, logger *slog.Logger) (Sender, error) {
	socket := os.Getenv(controlSocketEnv)
	if socket == "" {
		return nil, fmt.Errorf("rastrillo/mail: CARLOS_MAIL=submit but $%s "+
			"is not set; only the CARLOS agent should set CARLOS_MAIL",
			controlSocketEnv)
	}
	if os.Getenv(adminTokenEnv) == "" {
		return nil, fmt.Errorf("rastrillo/mail: CARLOS_MAIL=submit but $%s "+
			"is not set; restart the instance so the agent mints one",
			adminTokenEnv)
	}
	name := prefix + "_MAIL_FROM"
	from := strings.TrimSpace(os.Getenv(name))
	if from == "" {
		return nil, fmt.Errorf("rastrillo/mail: CARLOS_MAIL=submit but $%s "+
			"is not set; the platform delivers it when submission is on, "+
			"so check `carlos email status`", name)
	}
	if _, err := netmail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("rastrillo/mail: $%s is %q, not an email "+
			"address: %v", name, from, err)
	}
	logger.Info("rastrillo/mail: sending through CARLOS mail submission",
		"from", from)
	return Submit(socket, "", from), nil
}
