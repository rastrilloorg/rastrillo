package keyring

import _ "embed"

//go:embed js/keyring.mjs
var jsTwin []byte

// JS returns the raw bytes of the JS twin (js/keyring.mjs), an ES
// module over WebCrypto for apps to serve as a static asset — crypto's
// pattern exactly, app-owned after delivery. The twin imports
// ./crypto.mjs, so serve crypto.JS() beside it under the same mount:
// the sibling layout is the deployment contract, and js_test.go proves
// the pair works by materialising exactly that layout.
func JS() []byte { return jsTwin }
