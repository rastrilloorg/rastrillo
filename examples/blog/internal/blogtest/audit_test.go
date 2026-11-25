package blogtest

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/audit"

	"blog/internal/blog"
)

// A table nobody declared is one erasure and export would never reach.
func TestTablesAreDeclared(t *testing.T) {
	t.Parallel()
	audit.CheckSchema(t, blog.BootSchema)
}
