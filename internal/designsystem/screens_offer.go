package designsystem

import (
	"html/template"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/signin"
	qrcode "github.com/skip2/go-qrcode"
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
				Action: signin.AddAuthenticatorPath, QR: galleryQR(),
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

// galleryQR is galleryKey's QR code drawn as totp draws an enrolment's:
// one path of unit squares in currentColor, without the xmlns an HTML
// page does not need. A grey box where the code goes would not show
// how much room it takes.
func galleryQR() template.HTML {
	code, err := qrcode.New("otpauth://totp/Harbour:"+graceAddress+
		"?secret="+galleryKey+"&issuer=Harbour", qrcode.Medium)
	if err != nil {
		panic(err) // a fixed text always encodes
	}
	bits := code.Bitmap()
	var path strings.Builder
	for row, cells := range bits {
		for column, on := range cells {
			if on {
				path.WriteString("M" + strconv.Itoa(column) + " " +
					strconv.Itoa(row) + "h1v1h-1z")
			}
		}
	}
	size := strconv.Itoa(len(bits))
	return template.HTML(`<svg viewBox="0 0 ` + size + ` ` + size +
		`" role="img" aria-label="QR code for your authenticator app" ` +
		`shape-rendering="crispEdges"><path fill="currentColor" d="` +
		path.String() + `"/></svg>`)
}
