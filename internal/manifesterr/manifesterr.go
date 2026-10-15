// Package manifesterr holds the one sentinel error the root package's
// Resource.Validate and the manifest loader (internal/manifest) share.
// It imports only the standard library so both can import it without a
// cycle: internal/manifest already imports the root package for
// rastrillo.Resource, and the root package cannot import
// internal/manifest back.
//
// The error is not exported from the root package itself: a manifest
// author has no reason to errors.Is against it directly, and
// TestExportedSymbolsAreDocumented would otherwise require a reviewed
// reference entry for a symbol that exists purely to let one internal
// caller format one message correctly.
package manifesterr

import "errors"

// ErrNoCurrency is Resource.Validate's error for a resource with a
// money field but no default currency. internal/manifest.Load
// recognizes it with errors.Is to compose Appendix C.2's
// op.generate.no_currency line exactly, as one sentence,
// "manifest/<file>.toml has a money field...", instead of its usual
// "manifest/<file>.toml: <message>" (a colon would read as introducing
// a clause rather than finishing the sentence).
var ErrNoCurrency = errors.New(`has a money field but no currency; add currency = "EUR" (or another ISO 4217 code), the default for new records`)
