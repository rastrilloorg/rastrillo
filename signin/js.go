package signin

import _ "embed"

//go:embed js/reveal.mjs
var revealJS []byte

// RevealJS is the passphrase add page's show/hide module: it shows the
// page's "Show passphrase" switch, which ships hidden, and switches the
// field between text and password, back to password as the form is
// sent. Serve it at RevealModulePath with a JavaScript content type.
func RevealJS() []byte { return revealJS }
