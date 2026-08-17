package crypto

import _ "embed"

//go:embed js/crypto.mjs
var jsTwin []byte

// JS returns the raw bytes of the JS twin (js/crypto.mjs), an ES module
// implementing the same wire formats over WebCrypto, for apps to serve
// as a static asset — vendored the way ui vendors its partials, so the
// browser half of an E2EE flow is an embed away, not a hand-copy.
func JS() []byte { return jsTwin }
