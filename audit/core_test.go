package audit_test

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/audit"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/blobs"
	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/handoff"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/oauth"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/rootcall"
	"amadan.net/rastrillo/rastrillo/roster"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/signin"
	"amadan.net/rastrillo/rastrillo/totp"
)

func coreSchema() *migrate.Set {
	return migrate.Merge(sessions.Schema, auth.Schema, apitoken.Schema,
		blobs.Schema, eventlog.Schema, handoff.Schema, oauth.Schema,
		passkey.Schema, pow.Schema, rootcall.Schema, roster.Schema,
		secondfactor.Schema, signin.Schema, totp.Schema)
}

func TestCoreSchemasAreDeclared(t *testing.T) {
	audit.CheckSchema(t, coreSchema())
}

func TestCoreExemptionsHaveReasons(t *testing.T) {
	for _, ex := range audit.Exemptions(coreSchema()) {
		if ex.Reason == "" {
			t.Errorf("%s.%s: %s has no reason", ex.Namespace, ex.Table, ex.What)
		}
	}
}

func TestPasskeyWithoutSecondFactorIsDeclared(t *testing.T) {
	audit.CheckSchema(t,
		migrate.Merge(sessions.Schema, auth.Schema, passkey.Schema))
}
