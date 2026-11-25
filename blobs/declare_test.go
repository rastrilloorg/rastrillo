package blobs

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/migrate"
)

// Inline keeps the bytes themselves in this table, so they may be
// personal. With no owner column erasure cannot find them, and the
// declaration must say so as an exemption rather than call them shared.
func TestInlineBlobsAreKeptPersonalData(t *testing.T) {
	decls := Schema.Declarations()
	if len(decls) != 1 {
		t.Fatalf("declarations = %+v, want one for blobs", decls)
	}
	decl := decls[0]
	if decl.Kind != migrate.Personal || decl.Erase != migrate.EraseKeep ||
		decl.KeepReason == "" || decl.NotAudited != "" {
		t.Errorf("blobs is declared %+v, want personal, kept with a "+
			"reason, and audited", decl)
	}
}
