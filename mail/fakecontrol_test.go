package mail

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeAnswer is one scripted answer from the mailer behind the verb.
type fakeAnswer struct {
	status     int
	code       string
	retryAfter string
}

// fakeControl is a control socket that speaks POST /v1/mail as the
// CARLOS agent does at platform eeb5100a3 (internal/mailsubmit/verb.go):
// the bearer, two messages in flight, the envelope's shape, then the
// mailer's recipient and From rules, then the next scripted answer.
type fakeControl struct {
	socket string
	token  string

	mutex    sync.Mutex
	answers  []fakeAnswer
	received []platformEnvelopeWire
	raws     [][]byte
	inFlight int
	peak     int
	hold     chan struct{}
	// verbatim, when set, is the body of every 200 in place of the
	// platform's {"message_id"}, to test answers a healthy one never sends.
	verbatim *string
}

// newFakeControl serves on a socket in a short temporary directory:
// macOS refuses a socket path over 104 bytes, which t.TempDir passes.
func newFakeControl(t *testing.T, answers ...fakeAnswer) *fakeControl {
	t.Helper()
	dir, err := os.MkdirTemp("", "mailsock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	fake := &fakeControl{
		socket:  filepath.Join(dir, "control.sock"),
		token:   "test-admin-token",
		answers: answers,
	}
	listener, err := net.Listen("unix", fake.socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/mail", fake.serve)
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return fake
}

func (fake *fakeControl) serve(
	response http.ResponseWriter, request *http.Request,
) {
	got := []byte(request.Header.Get("Authorization"))
	if subtle.ConstantTimeCompare(got, []byte("Bearer "+fake.token)) != 1 {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !fake.enter() {
		fakeWrite(response, fakeAnswer{http.StatusTooManyRequests,
			CodeRate, "1"})
		return
	}
	defer fake.leave()
	env, raw, ok := platformEnvelope(response, request)
	_, fromOK := platformOneFrom(raw, env.From)
	if !ok || !platformRecipients(env.To) || !fromOK {
		fakeWrite(response, fakeAnswer{http.StatusBadRequest,
			CodeBadRequest, ""})
		return
	}
	if fake.hold != nil {
		<-fake.hold
	}
	answer := fake.next(env, raw)
	if fake.verbatim != nil && answer.status == http.StatusOK {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(*fake.verbatim))
		return
	}
	fakeWrite(response, answer)
}

// enter is the verb's in-flight limit of two per socket.
func (fake *fakeControl) enter() bool {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if fake.inFlight == 2 {
		return false
	}
	fake.inFlight++
	fake.peak = max(fake.peak, fake.inFlight)
	return true
}

func (fake *fakeControl) leave() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.inFlight--
}

// next records the message and pops the next answer; the last repeats,
// and with none scripted the answer is 200.
func (fake *fakeControl) next(
	env platformEnvelopeWire, raw []byte,
) fakeAnswer {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.received = append(fake.received, env)
	fake.raws = append(fake.raws, raw)
	if len(fake.answers) == 0 {
		return fakeAnswer{status: http.StatusOK}
	}
	answer := fake.answers[0]
	if len(fake.answers) > 1 {
		fake.answers = fake.answers[1:]
	}
	return answer
}

// requests is how many messages reached the mailer.
func (fake *fakeControl) requests() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return len(fake.received)
}

func fakeWrite(response http.ResponseWriter, answer fakeAnswer) {
	response.Header().Set("Content-Type", "application/json")
	if answer.retryAfter != "" {
		response.Header().Set("Retry-After", answer.retryAfter)
	}
	response.WriteHeader(answer.status)
	body := map[string]string{"code": answer.code}
	if answer.status == http.StatusOK {
		body = map[string]string{"message_id": "ses-" + time.Now().String()}
	}
	_ = json.NewEncoder(response).Encode(body)
}

// sender is a submit Sender wired to this fake.
func (fake *fakeControl) sender(from string) Sender {
	return Submit(fake.socket, fake.token, from)
}
