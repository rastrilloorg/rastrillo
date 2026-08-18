// The manifest system's generated templates, compiled into the binary
// for the same reason assets.go embeds static/ (blog's friction log
// F8). This file has to live here, at the module root, rather than
// under internal/tickets alongside the layout — go:embed patterns
// cannot contain ".." path elements, and internal/tickets is not an
// ancestor of gen/templates (they are siblings under the module
// root), so only a file rooted here can reach down into gen/templates
// — see examples/blog/genassets.go, the same reasoning.
//
// This file is hand-written, not generated: `rastrillo generate` only
// ever clears and rewrites gen/actions/ (see cmd/rastrillo/generate.go),
// never removes a file like this one sitting beside it, so it survives
// every regeneration undisturbed.
//
// Unlike blog's genassets.go, there is no AppTemplatesFS here: this
// app ejects nothing (see the package README — that absence is the
// whole point), so gen/templates is the only template source
// internal/tickets ever reads.
package tickets

import "embed"

//go:embed gen/templates
var GenTemplatesFS embed.FS
