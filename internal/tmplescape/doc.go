// Package tmplescape is html/template's escaping pass, forked from Go 1.26.10
// so the template compiler can read its output. EscapeAll takes a set's
// parse trees and returns, for each template run as an entry, the trees
// html/template would execute, or the error it would return.
//
// html/template cannot give the compiler those trees itself. When a
// template is called from a context other than plain HTML, such as a
// block inside <title>, escaping derives a copy of the callee for that
// context and adds it only to a private text/template namespace that
// Templates and Lookup do not reach. The fork returns derived templates
// with the rest.
//
// escape.go is the standard library's, with what a map of parse trees in
// place of a *Template requires:
//
//   - escapeTemplate takes the escaper rather than a *Template, and on an
//     error returns it without marking the template unusable: EscapeAll
//     escapes each entry on fresh copies, so nothing reuses the template.
//   - The escaper's nameSpace and the *text/template.Template it handles
//     are small stand-ins (escapeall.go). Looking a template up copies its
//     tree; commit adds derived templates to the namespace instead of
//     calling AddParseTree, and adds no funcs, since the escapers are
//     tmplrt's.
//   - The escaper funcMap, evalArgs, filterFailsafe and the forwarding
//     functions (HTMLEscape and so on) are gone: they belong to the
//     escapers, not the pass. evalArgs is tmplrt.EvalArgs.
//   - GODEBUG htmlmetacontenturlescape comes from Options instead of
//     internal/godebug, which is not importable. jstmpllitinterp was
//     declared and unused, and is gone.
//   - The context machinery is internal/htmlctx, shared with tmplrt,
//     so its names are capitalised and qualified there (htmlctx.StateText
//     for stateText; see htmlctx's doc.go), and delimEnds moved there.
//   - The ends-in-a-non-text-context Error literal has field keys, which
//     vet requires now that Error is another package's type.
//
// Error and ErrorCode are html/template's, defined in htmlctx and named
// here.
//
// The forked file carries the Go authors' copyright notice and is used
// under their BSD licence, reproduced in LICENSE-go in this directory.
package tmplescape
