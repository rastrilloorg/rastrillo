package carlos_test

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/carlos"
)

func TestHostReadsCarlosHost(t *testing.T) {
	t.Setenv("CARLOS_HOST", "acme-k3x9qa.bdf.oncarlos.com")
	if got := carlos.Host(); got != "acme-k3x9qa.bdf.oncarlos.com" {
		t.Fatalf("Host() = %q", got)
	}
	t.Setenv("CARLOS_HOST", "")
	if got := carlos.Host(); got != "" {
		t.Fatalf("Host() off CARLOS = %q, want empty", got)
	}
}
