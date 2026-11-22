package passkey

import _ "embed"

//go:embed js/signin.mjs
var signinJS []byte

// JS is the sign-in screen's passkey door: an ES module that reveals
// the signin partial's passkey button where a ceremony can run, runs
// the discover pair, and navigates only after a successful answer.
// Serve it with a JavaScript content type at the URL you set as
// auth.PasskeyDoor.ScriptURL, beside webauthn.JS(). The partial loads
// it only on a page that shows the passkey door.
func JS() []byte { return signinJS }

//go:embed js/register.mjs
var registerJS []byte

// RegisterJS is the passkey add page's script: an ES module that
// enables the page's button where a ceremony can run, creates a passkey
// through RegisterBegin and RegisterFinish, and navigates to the
// answer's "to" only after a successful one, or to the sign-in page a
// 401 names in "reauth". It imports JS() as
// "./signin.mjs", so serve the two side by side under those names, with
// a JavaScript content type.
func RegisterJS() []byte { return registerJS }
