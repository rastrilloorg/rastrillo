package signin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/webauthn"
)

// Mount registers the routes of the ways that are on, and none for a way
// that is off; call it where the app's gate leaves routes open, since
// the sign-in POSTs are for people not yet signed in. A nil *Signins,
// which rastrillo routes lists an empty App with, mounts every way's
// routes and serves none.
func (signins *Signins) Mount(routes *action.Routes) {
	ways := Passkey | Authenticator | Passphrase
	if signins != nil {
		ways = signins.config.Ways
	}
	if ways&Passkey != 0 {
		signins.mountPasskey(routes)
	}
	if ways&Authenticator != 0 {
		routes.Handle(http.MethodPost, CodePath,
			http.HandlerFunc(signins.SignInWithCode), "signInWithCode")
		signins.mountAdd(routes, AddAuthenticatorPath, "Authenticator",
			signins.ShowAddAuthenticator, signins.AddAuthenticator)
	}
	if ways&Passphrase != 0 {
		signins.mountPassphrase(routes)
	}
	if ways != 0 {
		signins.mountManagement(routes)
	}
}

func (signins *Signins) mountPasskey(routes *action.Routes) {
	handle := func(method string, path action.Path,
		handler http.HandlerFunc, name string,
	) {
		routes.Handle(method, path, handler, name)
	}
	handle(http.MethodPost, PasskeyBeginPath,
		signins.discoverBegin, "passkeySignInBegin")
	handle(http.MethodPost, PasskeyFinishPath,
		signins.discoverFinish, "passkeySignInFinish")
	handle(http.MethodGet, AddPasskeyPath,
		signins.ShowAddPasskey, "showAddPasskey")
	handle(http.MethodPost, RegisterBeginPath,
		signins.RegisterBegin, "passkeyRegisterBegin")
	handle(http.MethodPost, RegisterFinishPath,
		signins.RegisterFinish, "passkeyRegisterFinish")
	module(routes, WebauthnModulePath, webauthn.JS(), "webauthnModule")
	module(routes, PasskeyModulePath, passkey.JS(), "passkeyModule")
	module(routes, RegisterModulePath, passkey.RegisterJS(),
		"passkeyRegisterModule")
}

func (signins *Signins) mountPassphrase(routes *action.Routes) {
	routes.Handle(http.MethodPost, PassphrasePath,
		http.HandlerFunc(signins.SignInWithPassphrase),
		"signInWithPassphrase")
	signins.mountAdd(routes, AddPassphrasePath, "Passphrase",
		signins.ShowAddPassphrase, signins.AddPassphrase)
	module(routes, RevealModulePath, RevealJS(), "revealModule")
}

// discoverBegin and discoverFinish are passkey's discover pair, reached
// through signins when a request comes, so Mount reads nothing from a
// nil *Signins.
func (signins *Signins) discoverBegin(
	response http.ResponseWriter, request *http.Request,
) {
	signins.passkeys.DiscoverBegin(response, request)
}

func (signins *Signins) discoverFinish(
	response http.ResponseWriter, request *http.Request,
) {
	signins.passkeys.DiscoverFinish(response, request)
}

func (signins *Signins) mountAdd(
	routes *action.Routes, path action.Path, way string,
	show, save http.HandlerFunc,
) {
	routes.Handle(http.MethodGet, path, show, "showAdd"+way)
	routes.Handle(http.MethodPost, path, save, "add"+way)
}

func (signins *Signins) mountManagement(routes *action.Routes) {
	routes.Handle(http.MethodGet, OfferPath,
		http.HandlerFunc(signins.ShowOffer), "showOffer")
	routes.Handle(http.MethodPost, OfferLaterPath,
		http.HandlerFunc(signins.LaterOffer), "laterOffer")
	routes.Handle(http.MethodGet, OptionsPath,
		http.HandlerFunc(signins.ShowOptions), "signinOptions")
	routes.Handle(http.MethodPost, OptionsRemovePath,
		http.HandlerFunc(signins.RemoveWay), "removeSigninWay")
}

// module mounts a JavaScript module. Its URL carries no hash, so the
// browser keeps a copy but asks again each time, as it does for any
// asset served without one: the ETag answers that with a 304.
func module(
	routes *action.Routes, path action.Path, body []byte, name string,
) {
	sum := sha256.Sum256(body)
	tag := `"` + hex.EncodeToString(sum[:])[:16] + `"`
	routes.Handle(http.MethodGet, path, http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		header := response.Header()
		header.Set("Content-Type", "text/javascript; charset=utf-8")
		header.Set("Cache-Control", "no-cache")
		header.Set("ETag", tag)
		http.ServeContent(response, request, "", time.Time{},
			bytes.NewReader(body))
	}), name)
}
