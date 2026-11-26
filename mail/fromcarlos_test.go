package mail

import (
	"strings"
	"testing"
)

// setEnv sets every variable FromCarlos reads: those named in vars to
// their values, the rest to empty, so the machine's own cannot leak in.
func setEnv(t *testing.T, vars map[string]string) {
	for _, name := range []string{
		"CARLOS_MAIL", "CARLOS_CONTROL_SOCKET", "CARLOS_ADMIN_TOKEN",
		"APP_MAIL_FROM", "APP_SMTP_HOST", "APP_SMTP_FROM",
		"APP_SMTP_PORT", "APP_SMTP_USER", "APP_SMTP_PASS",
	} {
		t.Setenv(name, vars[name])
	}
}

var (
	submitVars = map[string]string{
		"CARLOS_CONTROL_SOCKET": "/run/carlos/control.sock",
		"CARLOS_ADMIN_TOKEN":    "token",
		"APP_MAIL_FROM":         "noreply@app.example",
	}
	smtpVars = map[string]string{
		"APP_SMTP_HOST": "email-smtp.example", "APP_SMTP_FROM": "a@b.example",
	}
)

func merged(sets ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, set := range sets {
		for name, value := range set {
			out[name] = value
		}
	}
	return out
}

func TestSubmitIsChosenOnlyByCarlosMailSubmit(t *testing.T) {
	both := merged(submitVars, smtpVars)
	for name, test := range map[string]struct {
		vars map[string]string
		want string
	}{
		"submit over SMTP": {
			merged(both, map[string]string{"CARLOS_MAIL": "submit"}),
			"submit"},
		"no CARLOS_MAIL": {both, "smtp"},
		"SMTP alone":     {smtpVars, "smtp"},
		"another value": {
			merged(both, map[string]string{"CARLOS_MAIL": "smtp"}),
			"smtp"},
		"not lowercase": {
			merged(both, map[string]string{"CARLOS_MAIL": "SUBMIT"}),
			"smtp"},
	} {
		t.Run(name, func(t *testing.T) {
			setEnv(t, test.vars)
			sender, err := FromCarlos("APP", nil)
			if err != nil {
				t.Fatalf("FromCarlos: %v", err)
			}
			if got := transportName(sender); got != test.want {
				t.Errorf("transport = %s, want %s", got, test.want)
			}
		})
	}
}

func TestSubmitUsesTheMailFromVariable(t *testing.T) {
	setEnv(t, merged(submitVars, smtpVars,
		map[string]string{"CARLOS_MAIL": "submit"}))
	sender, err := FromCarlos("APP", nil)
	if err != nil {
		t.Fatal(err)
	}
	if from := sender.(*submitSender).from; from != "noreply@app.example" {
		t.Errorf("from = %q, want APP_MAIL_FROM's", from)
	}
}

// An app built on FromEnv sends through CARLOS once mail is switched on
// there, and a half-configured submit still lets it start.
func TestFromEnvUsesCarlosMailWhenItIsOn(t *testing.T) {
	setEnv(t, merged(submitVars, smtpVars,
		map[string]string{"CARLOS_MAIL": "submit"}))
	if got := transportName(FromEnv("APP", nil)); got != "submit" {
		t.Errorf("FromEnv chose %s with CARLOS mail on, want submit", got)
	}
	without := merged(submitVars, smtpVars,
		map[string]string{"CARLOS_MAIL": "submit"})
	delete(without, "APP_MAIL_FROM")
	setEnv(t, without)
	if got := transportName(FromEnv("APP", nil)); got != "smtp" {
		t.Errorf("FromEnv chose %s with submit half-configured, want smtp",
			got)
	}
}

func TestFromCarlosRefusesToBootWithoutAChannel(t *testing.T) {
	submit := merged(submitVars, map[string]string{"CARLOS_MAIL": "submit"})
	without := func(name string) map[string]string {
		vars := merged(submit, smtpVars)
		delete(vars, name)
		return vars
	}
	for name, test := range map[string]struct {
		vars map[string]string
		says string
	}{
		"no socket": {without("CARLOS_CONTROL_SOCKET"), "CARLOS_CONTROL_"},
		"no token":  {without("CARLOS_ADMIN_TOKEN"), "CARLOS_ADMIN_TOKEN"},
		"no From":   {without("APP_MAIL_FROM"), "APP_MAIL_FROM"},
		"bad From": {merged(submit, map[string]string{
			"APP_MAIL_FROM": "noreply"}), "APP_MAIL_FROM"},
		"neither": {map[string]string{}, "APP_SMTP_HOST"},
		"no SMTP From": {map[string]string{"APP_SMTP_HOST": "relay"},
			"APP_SMTP_FROM"},
	} {
		t.Run(name, func(t *testing.T) {
			setEnv(t, test.vars)
			sender, err := FromCarlos("APP", nil)
			if err == nil {
				t.Fatalf("FromCarlos chose %s, want a refusal",
					transportName(sender))
			}
			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("err = %v, want it to name %s", err, test.says)
			}
		})
	}
}

func transportName(sender Sender) string {
	switch sender.(type) {
	case *submitSender:
		return "submit"
	case *smtpSender:
		return "smtp"
	case loggedSender:
		return "logged"
	}
	return "unknown"
}
