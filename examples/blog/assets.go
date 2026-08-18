// The blog's static files, compiled into the binary: the platform
// deploys the binary alone, so a loose static/ directory would not
// travel with it (friction log F8).
package blog

import "embed"

//go:embed static
var StaticFS embed.FS
