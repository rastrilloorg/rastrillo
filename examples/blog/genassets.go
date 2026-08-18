// The manifest system's generated templates, compiled into the binary
// for the same reason assets.go embeds static/: the platform deploys
// the binary alone (friction log F8). This file has to live here,
// at the module root, rather than under internal/blog alongside the
// hand-written page templates — go:embed patterns cannot contain ".."
// path elements, and internal/blog is not an ancestor of gen/templates
// (they are siblings under the module root), so only a file rooted
// here can reach down into gen/templates. internal/blog's render
// adapter (view.go/genrender.go) imports this package for
// GenTemplatesFS and AppTemplatesFS.
//
// This file is hand-written, not generated: `rastrillo generate` only
// ever clears and rewrites gen/actions/ (see cmd/rastrillo/generate.go),
// never removes a file like this one sitting beside it, so it survives
// every regeneration undisturbed.
package blog

import "embed"

//go:embed gen/templates
var GenTemplatesFS embed.FS

// AppTemplatesFS embeds templates/ — the app-root tree a manifest
// resource's screen lands in once ejected (task 11:
// templates/posts/list.html, templates/posts/form.html). The same
// ".." restriction that forces GenTemplatesFS to live here applies
// equally to an ejected file: it sits beside gen/, not under
// internal/blog, so internal/blog cannot embed it directly either.
//
//go:embed templates
var AppTemplatesFS embed.FS
