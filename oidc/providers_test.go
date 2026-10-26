package oidc

import "testing"

// TestIssuerMustMatchKind proves issuerMatchesKind's refusal actually
// fires — the one test in this package that runs with
// skipIssuerShapeCheck OFF, so TestMain's blanket bypass does not make
// this the "guard nothing can point at the defect" shape it exists to
// avoid (code review finding 2).
func TestIssuerMustMatchKind(t *testing.T) {
	old := skipIssuerShapeCheck
	skipIssuerShapeCheck = false
	t.Cleanup(func() { skipIssuerShapeCheck = old })

	ms, _ := ProviderFor(Microsoft)
	cases := []struct {
		name   string
		issuer string
		ok     bool
	}{
		{"a well-formed microsoft issuer", "https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0", true},
		{"google's issuer under a microsoft-kind connection", "https://accounts.google.com", false},
		{"an arbitrary issuer", "https://evil.example", false},
		{"the consumer tenant GUID's issuer shape", "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := issuerMatchesKind(ms, tc.issuer)
			if tc.ok && err != nil {
				t.Fatalf("expected this to pass, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected this to be refused")
			}
		})
	}

	google, _ := ProviderFor(Google)
	if err := issuerMatchesKind(google, "https://accounts.google.com"); err != nil {
		t.Fatalf("expected google's real issuer to pass, got %v", err)
	}
	if err := issuerMatchesKind(google, "https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0"); err == nil {
		t.Fatal("expected a microsoft issuer under a google-kind connection to be refused")
	}
}

func TestRefuseMicrosoftDirectoryRefusesTheConsumerTenantGUID(t *testing.T) {
	if why := refuseMicrosoftDirectory(consumersTenantID); why == "" {
		t.Fatal("expected the well-known consumer tenant GUID to be refused by name")
	}
}
