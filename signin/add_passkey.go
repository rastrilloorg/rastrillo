package signin

import (
	"bytes"
	"encoding/json"
	"net/http"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// ShowAddPasskey is GET AddPasskeyPath: the button that runs
// RegisterBegin and RegisterFinish, or, with done=1 and a passkey on
// the account, the line that says it worked.
func (signins *Signins) ShowAddPasskey(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.adding(response, request, Passkey)
	if !ok {
		return
	}
	to := returnTo(request)
	add := Add{Way: Passkey, ReturnTo: to,
		Address: signins.address(request, session.Subject),
		Passkey: &auth.PasskeyDoor{
			BeginPath:  RegisterBeginPath + carry(to),
			FinishPath: RegisterFinishPath + carry(to),
			ModuleURL:  WebauthnModulePath, ScriptURL: RegisterModulePath,
			ProbeURL: PasskeyProbePath,
		}}
	if request.URL.Query().Get("done") == "1" {
		enrolled, err := signins.passkeys.Enrolled(session.Subject)
		if err != nil {
			signins.broke(response, "look up passkeys", err)
			return
		}
		add.Done = enrolled
	}
	signins.render(response, request, http.StatusOK, add)
}

// RegisterBegin is POST RegisterBeginPath: passkey's, behind the same
// strong session the add page needs. It answers a fetch, so a refusal
// is a 401 naming the sign-in page, which the page's script goes to.
func (signins *Signins) RegisterBegin(
	response http.ResponseWriter, request *http.Request,
) {
	if signins.registering(response, request) {
		signins.passkeys.RegisterBegin(response, request)
	}
}

// RegisterFinish is POST RegisterFinishPath: passkey's, whose answer
// gains "to", the add page saying it is done, for the page to go to.
// Its session rotation passes through untouched, even when the offer's
// answer cannot be kept and the new passkey is taken back out.
func (signins *Signins) RegisterFinish(
	response http.ResponseWriter, request *http.Request,
) {
	if !signins.registering(response, request) {
		return
	}
	session, _ := signins.session(request)
	answer := &caught{ResponseWriter: response, status: http.StatusOK}
	signins.passkeys.RegisterFinish(answer, request)
	var finished map[string]any
	if answer.status != http.StatusOK ||
		json.Unmarshal(answer.body.Bytes(), &finished) != nil ||
		finished["ok"] != true {
		response.WriteHeader(answer.status)
		_, _ = response.Write(answer.body.Bytes())
		return
	}
	if !signins.answered(response, request, session.Subject, func() error {
		return signins.forgetNewPasskey(session.Subject, finished)
	}) {
		return
	}
	// From the query: the body was passkey's JSON, and is spent.
	to := sessions.SafeReturnTo(request.URL.Query().Get("return_to"), "")
	finished["to"] = AddPasskeyPath + "?done=1" + carryOn(to)
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(finished)
}

// forgetNewPasskey removes the passkey RegisterFinish just stored. One
// that was registered already is left alone: it was not this request's
// to take back.
func (signins *Signins) forgetNewPasskey(
	subject string, finished map[string]any,
) error {
	id, _ := finished["id"].(string)
	if finished["existing"] == true || id == "" {
		return nil
	}
	return signins.passkeys.Remove(subject, id)
}

// carryOn is carry for a query that already has a parameter.
func carryOn(returnTo string) string {
	if returnTo == "" {
		return ""
	}
	return "&" + carry(returnTo)[1:]
}

// registering is whether the request may run the passkey enrolment,
// having answered it if not. A session that is missing or no longer
// strong gets a 401 whose "reauth" is the sign-in page, returning to
// the add page with the return_to the request carried.
func (signins *Signins) registering(
	response http.ResponseWriter, request *http.Request,
) bool {
	if signins.config.Ways&Passkey == 0 {
		http.NotFound(response, request)
		return false
	}
	session, ok := signins.session(request)
	if ok && signins.strong(session) {
		return true
	}
	// From the query: the body is passkey's JSON, still to be read.
	to := sessions.SafeReturnTo(request.URL.Query().Get("return_to"), "")
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(response).Encode(map[string]string{
		"reauth": signins.signinURL(AddPasskeyPath+carry(to), ok,
			ok && mustReauth(session)),
	})
	return false
}

// caught holds a handler's status and body for the caller to finish,
// while headers, the session cookie among them, go straight through.
type caught struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (answer *caught) WriteHeader(status int) { answer.status = status }

func (answer *caught) Write(written []byte) (int, error) {
	return answer.body.Write(written)
}
