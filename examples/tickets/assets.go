// The tickets example's static files, compiled into the binary: the
// platform deploys the binary alone, so a loose static/ directory
// would not travel with it (blog's friction log F8 — see
// examples/blog/assets.go, the same fix, copied here for the same
// reason).
package tickets

import "embed"

//go:embed static
var StaticFS embed.FS
