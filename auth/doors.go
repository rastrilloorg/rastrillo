package auth

import "html/template"

// The doors a page sets on SigninState, as it sets Passkey: each is
// a way in the app mounted somewhere auth cannot know, so the page
// says where, and a nil door is a way the page does not offer.

// PasskeyDoor is where the app mounted passkey discovery (BeginPath,
// FinishPath), where it serves webauthn.JS() (ModuleURL, the WebAuthn
// helper), passkey.JS() (ScriptURL, the door's own script) and
// passkey.ProbeJS() (ProbeURL), and passkey.Config.LegacyRPID, if any.
// The partial loads the two scripts only when it renders the door.
type PasskeyDoor struct {
	BeginPath, FinishPath string
	// ModuleURL must be an absolute path ("/static/webauthn.mjs"), not a
	// relative one. The door's script import()s it, and import() resolves
	// "./x.mjs" against the importing module's own URL, not the page's,
	// and refuses "static/x.mjs" as a bare specifier: either way the
	// import fails and the button stays hidden on every page, with no
	// error anyone sees.
	ModuleURL string
	ScriptURL string
	// ProbeURL's script shows the door, shipped hidden, before the first
	// paint, so that nothing on the screen moves when it appears. Without
	// it the door's module shows it late, as before discussion #433.
	// Serve it fingerprinted and cached: the card waits for it.
	ProbeURL   string
	LegacyRPID string
}

// FederatedDoor is a sign-in another service runs, which the page
// supplies as it supplies Passkey: auth knows nothing of where the app
// mounted it.
type FederatedDoor struct {
	// Provider is the lastsignin provider this door's sign-in is
	// remembered under (lastsignin.ProviderOficina), and the ?way= that
	// picks it.
	Provider string
	// Href is where the button goes, by GET. The screen adds the page's
	// return_to to it.
	Href string
	// Label is the provider's name, "Oficina ID": the button reads
	// "Continue with" it, and the chooser's row reads it alone.
	Label string
	// Icon is optional: the provider's glyph as inline SVG drawn in
	// currentColor and marked aria-hidden, which lands as written.
	Icon template.HTML
}

// SecretDoor is the authenticator app's or the passphrase's way in:
// the address and the secret, posted together to Path, where the app
// mounted that way's sign-in. Both ask for the address because neither
// secret names its owner, as a passkey does.
type SecretDoor struct {
	Path string
}

// keepsSecretDoor is whether the page's problem is drawn on the secret
// door it came from: a wrong code, a wrong passphrase or a paused
// address, which the visitor answers by retyping the secret or by the
// door's link instead, not by retyping the address the way an email
// problem is answered on the field.
func (st SigninState) keepsSecretDoor() bool {
	switch st.Problem {
	case ProblemCode, ProblemPassphrase, ProblemPaused:
	default:
		return false
	}
	way := st.currentWay()
	return st.OneWay && (way == WayAuthenticator || way == WayPassphrase)
}
