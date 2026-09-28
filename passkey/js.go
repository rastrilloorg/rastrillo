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
