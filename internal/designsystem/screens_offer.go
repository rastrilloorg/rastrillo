package designsystem

import (
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/signin"
)

// offerScreenDocs are the signin package's pages after a sign-in: the
// offer of a faster way in, each way's add page, and the line that says
// a way was added. They are signin-view's, drawn from a signin.View.
func offerScreenDocs() []screenDoc {
	every := signin.Passkey | signin.Authenticator | signin.Passphrase
	return []screenDoc{
		{Key: "signin-offer", Name: "Offering a faster way to sign in",
			Blurb: "Show this to someone after an email-link sign-in, " +
				"with the passkey first. If they choose Not now, " +
				"don't show this page again.",
			View: &signin.View{Offer: &signin.Offer{Ways: every,
				LaterPath: signin.OfferLaterPath}}},
		{Key: "signin-add-passkey", Name: "Adding a passkey",
			Blurb: "Add one button so people can create a passkey " +
				"with their face, fingerprint, or screen lock.",
			View: addView(signin.Add{Way: signin.Passkey,
				Passkey: &auth.PasskeyDoor{}})},
		{Key: "signin-add-authenticator",
			Name: "Adding an authenticator app",
			Blurb: "Show the QR code and setup key, then ask for a " +
				"code to confirm the authenticator app works.",
			View: addView(signin.Add{Way: signin.Authenticator,
				Action: signin.AddAuthenticatorPath, URI: galleryURI,
				Key: galleryKey})},
		{Key: "signin-add-passphrase", Name: "Setting a passphrase",
			Blurb: "Require at least 15 characters. Don't require " +
				"digits, symbols, or capitals.",
			View: addView(signin.Add{Way: signin.Passphrase,
				Action: signin.AddPassphrasePath, Address: graceAddress})},
		{Key: "signin-added", Name: "A sign-in method added",
			Blurb: "Confirm the sign-in method was added, then return " +
				"to the page the person was trying to reach.",
			View: addView(signin.Add{Way: signin.Passkey, Done: true,
				ReturnTo: "/"})},
	}
}

func addView(add signin.Add) *signin.View {
	return &signin.View{Add: &add}
}

// galleryKey is a made-up authenticator key, the length a real one is.
const galleryKey = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

// galleryURI is galleryKey as the otpauth:// link its QR code draws.
const galleryURI = "otpauth://totp/Harbour:" + graceAddress +
	"?secret=" + galleryKey + "&issuer=Harbour"
