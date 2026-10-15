// Package moneycldr is the evidence behind
// docs/superpowers/specs/2026-10-09-multi-currency-money-design.md:
// the CLDR and ISO 4217 data generator and the comparison harness for
// multi-currency money formatting. It is a separate module so that the
// root gate never builds it and a download never becomes part of
// `make ci`. The shipped currency and locales packages are what the
// oracle checks.
//
//	make money-cldr                      # fetch (hash-checked), regenerate, test, compare, measure
//	cd hack/moneycldr && go test ./...   # offline: the side-flip data check
//
// The pieces:
//
//   - cmd/fetch downloads the pinned cldr-json files and archived ISO
//     4217 editions into a cache directory and checks them against
//     sources/inputs.sha256, so every run reads the same bytes.
//   - cmd/gen turns them into the root module's locales/core_gen.go
//     (root locale, lookup tables, the storage set), one pack per base
//     language (locales/<lang>), the all-modern convenience pack
//     (locales/modern), and testdata/storage.tsv and testdata/packs.tsv
//     here, and checks that every locale rebuilds exactly from its
//     parent diffs.
//   - cmd/check writes the comparison grid and reads it back after
//     oracle/intl.js has formatted it with ICU (Node's Intl), an
//     implementation independent of currency.Format. Every differing
//     row must be explained by an executable check
//     (cmd/check/exemptions.go), and the oracle must have answered
//     exactly the grid.
//   - cmd/packsize measures each pack's binary cost; cmd/xtext
//     re-measures the rejected golang.org/x/text route.
//
// The CLDR data is © Unicode, Inc., under the Unicode License v3 in
// locales/LICENSE-UNICODE.
package moneycldr
