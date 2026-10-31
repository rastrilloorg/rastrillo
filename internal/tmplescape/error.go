package tmplescape

import "amadan.net/rastrillo/rastrillo/internal/htmlctx"

// Error and ErrorCode are html/template's (error.go), forked into htmlctx
// because the transition functions there raise them. They are named here
// too so that EscapeAll's callers need only this package.
type (
	Error     = htmlctx.Error
	ErrorCode = htmlctx.ErrorCode
)

// The codes have html/template's values, so a code compares equal to the
// stdlib's once converted: int(e.ErrorCode) == int(template.ErrBranchEnd).
// ErrJSTemplate is left out: html/template no longer returns it.
const (
	OK                   = htmlctx.OK
	ErrAmbigContext      = htmlctx.ErrAmbigContext
	ErrBadHTML           = htmlctx.ErrBadHTML
	ErrBranchEnd         = htmlctx.ErrBranchEnd
	ErrEndContext        = htmlctx.ErrEndContext
	ErrNoSuchTemplate    = htmlctx.ErrNoSuchTemplate
	ErrOutputContext     = htmlctx.ErrOutputContext
	ErrPartialCharset    = htmlctx.ErrPartialCharset
	ErrPartialEscape     = htmlctx.ErrPartialEscape
	ErrRangeLoopReentry  = htmlctx.ErrRangeLoopReentry
	ErrSlashAmbig        = htmlctx.ErrSlashAmbig
	ErrPredefinedEscaper = htmlctx.ErrPredefinedEscaper
)
