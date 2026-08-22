// The manifest system's generated templates, compiled into the binary
// the way every rastrillo app embeds its assets. This file lives at
// the module root, not under internal/notes beside the hand templates,
// because go:embed patterns cannot contain ".." path elements and
// gen/templates is not under internal/notes — see examples/tickets's
// genassets.go, the same reasoning.
//
// Hand-written, not generated: `rastrillo generate` only ever clears
// and rewrites gen/actions/, never a file sitting beside gen/, so this
// survives every regeneration undisturbed.
package notesassets

import "embed"

//go:embed gen/templates
var GenTemplatesFS embed.FS
