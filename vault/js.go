package vault

import _ "embed"

//go:embed js/vault.mjs
var jsTwin []byte

// JS returns the raw bytes of the JS twin (js/vault.mjs), an ES
// module for apps to serve as a static asset — keyring's pattern
// exactly. The twin imports ./crypto.mjs, so serve crypto.JS() beside
// it under the same mount; js_test.go proves that sibling layout, and
// the Go-sealed fixture it opens proves the two sides agree.
func JS() []byte { return jsTwin }
