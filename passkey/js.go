package passkey

import _ "embed"

//go:embed js/signin.mjs
var signinJS []byte

// JS is the sign-in screen's passkey door: an ES module that runs the
// discover pair when the signin partial's passkey button is pressed,
// and navigates only after a successful answer.
// Serve it with a JavaScript content type at the URL you set as
// auth.PasskeyDoor.ScriptURL, beside webauthn.JS(). The partial loads
// it only on a page that shows the passkey door.
func JS() []byte { return signinJS }

//go:embed js/register.mjs
var registerJS []byte

// RegisterJS is the passkey add page's script: an ES module that,
// when the page's button is pressed, creates a passkey
// through RegisterBegin and RegisterFinish, and navigates to the
// answer's "to" only after a successful one, or to the sign-in page a
// 401 names in "reauth". It imports JS() as
// "./signin.mjs", so serve the two side by side under those names, with
// a JavaScript content type.
func RegisterJS() []byte { return registerJS }

//go:embed js/probe.js
var probeJS []byte

// ProbeJS is the classic script that readies the passkey buttons before
// the first paint: it shows the sign-in screen's door and enables the
// add page's button where a passkey can work, and keeps a press made
// before their modules load. Serve it with a JavaScript content type at
// the URL you set as auth.PasskeyDoor.ProbeURL. Without it the modules
// show the door and enable the button late, once they load.
func ProbeJS() []byte { return probeJS }
