package vectors

import _ "embed"

//go:embed js/vectors.mjs
var jsHelper []byte

// JS returns the raw bytes of the vendored JS helper
// (js/vectors.mjs): loadVectors and canonical, the vocabulary the
// scaffolded parity suite imports by name. `rastrillo vectors -init`
// delivers it to an app once as test/vectors.mjs, app-owned after —
// the tokens.css/shim contract — with a byte-identity pin scaffolded
// alongside so vendored-then-forgotten is caught instead of drifting
// silently.
func JS() []byte { return jsHelper }
