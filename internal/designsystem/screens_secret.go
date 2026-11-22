package designsystem

import "amadan.net/rastrillo/rastrillo/auth"

// secretScreenDocs are the one-way screen's authenticator and
// passphrase doors on the Screens page, each remembered and each with
// its own problem.
func secretScreenDocs() []screenDoc {
	return []screenDoc{
		{Key: "signin-authenticator", Name: "An authenticator app",
			Blurb: "Enter the code from your authenticator app, or " +
				"choose the email link to sign in another way.",
			Signin: secretScreen("totp", func(*auth.SigninState) {})},
		{Key: "signin-problem-code", Name: "A code that did not work",
			Blurb: "Enter the latest code, or choose the email link to " +
				"sign in another way.",
			Signin: secretScreen("totp", secretProblem(
				auth.WayAuthenticator, auth.ProblemCode))},
		{Key: "signin-passphrase", Name: "A passphrase",
			Blurb: "Let your password manager fill the email and " +
				"passphrase fields.",
			Signin: secretScreen("passphrase", func(*auth.SigninState) {})},
		{Key: "signin-problem-passphrase",
			Name: "A passphrase that did not match",
			Blurb: "Use one error for an unknown address or a wrong " +
				"passphrase.",
			Signin: secretScreen("passphrase", secretProblem(
				auth.WayPassphrase, auth.ProblemPassphrase))},
	}
}

// A Preview's forms go nowhere, so these paths need not exist.
var (
	galleryCode       = &auth.SecretDoor{Path: "/signin/code"}
	galleryPassphrase = &auth.SecretDoor{Path: "/signin/passphrase"}
)

// secretScreen is a browser that last signed in as grace by method.
func secretScreen(
	method string, change func(*auth.SigninState),
) *auth.SigninState {
	return signinScreen(func(state *auth.SigninState) {
		state.OneWay, state.SigninPath = true, "/signin"
		state.Authenticator = galleryCode
		state.Passphrase = galleryPassphrase
		state.Step, state.Address = auth.StepReturning, graceAddress
		state.Remembered = &auth.Remembered{Method: method,
			Address: graceAddress}
		change(state)
	})
}

func secretProblem(
	way auth.SigninWay, problem auth.SigninProblem,
) func(*auth.SigninState) {
	return func(state *auth.SigninState) {
		state.Step, state.Way, state.Problem = auth.StepAsk, way, problem
	}
}
