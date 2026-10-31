// Package htmlctx is html/template's model of where in an HTML document a
// byte falls: the context type, the transition functions that carry it
// across a run of text, and the escape error codes those functions
// raise. It is forked from html/template in Go 1.26.10 for Rastrillo's
// compiled templates.
//
// Two packages walk this state machine, and there must be one copy of
// it, or the two can drift apart: internal/tmplescape decides from it
// which escapers each action needs, and tmplrt's attribute escaper uses
// it, through StripTags, to strip the tags from template.HTML.
//
// The code is the standard library's. To be reachable from those two
// packages, every identifier they use, and every identifier starting
// with one of the context's type names, is capitalised: context becomes
// Context, c.state c.State, stateText StateText, errorf Errorf, and the
// fields and types jsCtx and urlPart become JSCtx and URLPart. Comments
// and string literals are untouched, so a comment may still name
// stateText. Re-syncing from a new Go release is copying the files again
// and repeating that rename; TestForkMatchesToolchain in
// internal/tmplescape lists the files. The *_string.go files are the
// stdlib's stringer output, renamed the same way. The stdlib's generate
// directives in context.go are dropped, since they name the old types.
//
// The forked files carry the Go authors' copyright notice and are used
// under their BSD licence, reproduced in LICENSE-go in this directory.
package htmlctx
