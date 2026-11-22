package signin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
)

func TestFiveMissesPauseTheAccount(t *testing.T) {
	test := newRig(t, nil)
	key := test.enrol(ada)
	start := test.clock.now
	for range 5 {
		test.post(test.signins.SignInWithCode,
			url.Values{"address": {ada}, "code": {test.wrongCode(key)}})
	}
	test.clock.now = start.Add(15*time.Minute - time.Second)
	right := url.Values{"address": {" ada@EXAMPLE.com "},
		"code": {test.code(key)}}
	response := test.post(test.signins.SignInWithCode, right)
	if query(t, response).Get("err") != "paused" {
		t.Fatalf("sixth attempt landed on %q, want err=paused",
			response.Header().Get("Location"))
	}
	test.clock.now = start.Add(15*time.Minute + time.Second)
	right.Set("address", ada)
	response = test.post(test.signins.SignInWithCode, right)
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/" {
		t.Fatalf("after the pause: %d to %q, want 303 to /", response.Code,
			response.Header().Get("Location"))
	}
}

func TestThirtyMissesPauseTheIP(t *testing.T) {
	test := newRig(t, nil)
	test.setPassphrase(ada, adaSecret)
	for at := range 30 {
		test.post(test.signins.SignInWithPassphrase, url.Values{
			"address":    {fmt.Sprintf("guess%d@example.com", at)},
			"passphrase": {wrongWords}})
	}
	right := url.Values{"address": {ada}, "passphrase": {adaSecret}}
	response := test.post(test.signins.SignInWithPassphrase, right)
	if query(t, response).Get("err") != "paused" {
		t.Fatalf("31st attempt landed on %q, want err=paused",
			response.Header().Get("Location"))
	}
	response = test.postFrom("198.51.100.7",
		test.signins.SignInWithPassphrase, right)
	if response.Header().Get("Location") != "/" {
		t.Fatalf("another address's attempt landed on %q, want /",
			response.Header().Get("Location"))
	}
}

func TestPausesSurviveANewSigns(t *testing.T) {
	test := newRig(t, nil)
	test.setPassphrase(ada, adaSecret)
	for range 5 {
		test.post(test.signins.SignInWithPassphrase,
			url.Values{"address": {ada}, "passphrase": {wrongWords}})
	}
	config := baseConfig(t, test.database, test.auth, test.clock)
	again, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	response := test.post(again.SignInWithPassphrase,
		url.Values{"address": {ada}, "passphrase": {adaSecret}})
	if query(t, response).Get("err") != "paused" {
		t.Fatalf("a new Signins let %q through to %q", ada,
			response.Header().Get("Location"))
	}
}

func TestSweepForgetsOnlyMissesPastTheWindow(t *testing.T) {
	test := newRig(t, nil)
	test.post(test.signins.SignInWithPassphrase,
		url.Values{"address": {ada}, "passphrase": {wrongWords}})
	start := test.clock.now
	if err := test.signins.Sweep(start.Add(missWindow)); err != nil {
		t.Fatal(err)
	}
	if test.misses() != 2 {
		t.Fatalf("%d failures after a sweep inside the window, want 2",
			test.misses())
	}
	if err := test.signins.Sweep(start.Add(missWindow + 1)); err != nil {
		t.Fatal(err)
	}
	if test.misses() != 0 {
		t.Fatalf("%d failures after the window, want 0", test.misses())
	}
}

// TestAnAttemptInFlightCountsAgainstTheBudget holds the fifth attempt
// between its reservation and its check, and sends a sixth through a
// second Signins on the same database meanwhile: it must be paused,
// not handed the guess the fifth has not spent yet.
func TestAnAttemptInFlightCountsAgainstTheBudget(t *testing.T) {
	test := newRig(t, nil)
	test.setPassphrase(ada, adaSecret)
	wrong := url.Values{"address": {ada}, "passphrase": {wrongWords}}
	for range accountMisses - 1 {
		test.post(test.signins.SignInWithPassphrase, wrong)
	}
	second, err := New(baseConfig(t, test.database, test.auth, test.clock))
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	check := test.signins.checkPassphrase
	test.signins.checkPassphrase = func(
		ctx context.Context, subject, secret string,
	) (bool, error) {
		close(entered)
		<-release
		return check(ctx, subject, secret)
	}
	held := make(chan *httptest.ResponseRecorder)
	go func() { held <- test.post(test.signins.SignInWithPassphrase, wrong) }()
	<-entered
	response := test.post(second.SignInWithPassphrase,
		url.Values{"address": {ada}, "passphrase": {adaSecret}})
	close(release)
	fifth := <-held
	if query(t, response).Get("err") != "paused" {
		t.Errorf("the sixth attempt landed on %q, want err=paused",
			response.Header().Get("Location"))
	}
	if query(t, fifth).Get("err") != "passphrase" {
		t.Errorf("the fifth attempt landed on %q, want err=passphrase",
			fifth.Header().Get("Location"))
	}
}

func TestASuccessGivesItsReservationBack(t *testing.T) {
	test := newRig(t, nil)
	test.setPassphrase(ada, adaSecret)
	response := test.post(test.signins.SignInWithPassphrase,
		url.Values{"address": {ada}, "passphrase": {adaSecret}})
	if response.Header().Get("Location") != "/" || test.misses() != 0 {
		t.Fatalf("a sign-in to %q left %d failures, want / and 0",
			response.Header().Get("Location"), test.misses())
	}
}

// TestALateReleaseSparesNewerMisses is a success that outlives the
// window: Sweep takes its rows, a newer attempt fails and is counted,
// and only then does the success give its reservation back. That must
// not take the newer attempt's misses with it.
func TestALateReleaseSparesNewerMisses(t *testing.T) {
	test := newRig(t, nil)
	ctx, start := context.Background(), test.clock.now
	buckets := []bucket{accountBucket(ada, ada), clientBucket("192.0.2.1")}
	first, _, err := test.signins.limit.reserve(ctx, start, buckets)
	if err != nil {
		t.Fatal(err)
	}
	later := start.Add(missWindow + time.Second)
	if err := test.signins.Sweep(later); err != nil {
		t.Fatal(err)
	}
	_, _, err = test.signins.limit.reserve(ctx, later, buckets)
	if err != nil {
		t.Fatal(err)
	}
	if err := test.signins.limit.release(ctx, first); err != nil {
		t.Fatal(err)
	}
	if test.misses() != 2 {
		t.Fatalf("%d failures left after a late release, want the newer "+
			"attempt's 2", test.misses())
	}
}

// Five misses are a subject's, not an address's: two addresses that
// sign in as one person, guessed from different clients, share them.
func TestAliasesOfOneSubjectShareFiveMisses(t *testing.T) {
	const person = "user:ana"
	aliases := []string{"ana@example.com", "ana@alias.example"}
	database := schema.Open(t)
	authority := newAuth(t, database, func(config *auth.Config) {
		config.SubjectFor = func(string) (string, error) {
			return person, nil
		}
	})
	moment := &clock{now: time.Now()}
	config := baseConfig(t, database, authority, moment)
	config.Ways = Authenticator
	config.Lookup = func(context.Context, string) (string, bool, error) {
		return person, true, nil
	}
	signins, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	test := &rig{t: t, database: database, auth: authority,
		signins: signins, clock: moment}
	key := test.enrol(person)
	for miss := range accountMisses {
		test.postFrom(fmt.Sprintf("192.0.2.%d", miss+1),
			signins.SignInWithCode, url.Values{
				"address": {aliases[miss%2]}, "code": {test.wrongCode(key)}})
	}
	response := test.postFrom("198.51.100.7", signins.SignInWithCode,
		url.Values{"address": {aliases[1]}, "code": {test.code(key)}})
	if query(t, response).Get("err") != "paused" {
		t.Fatalf("the sixth guess landed on %q, want err=paused",
			response.Header().Get("Location"))
	}
}

// Every subject has its own five, holding the secret or not, so how a
// pause falls across two spellings of an address says nothing about
// who enrolled: Ada has an authenticator, Bea has none, and the other
// spelling answers alike for both.
func TestAPauseTellsNothingAboutEnrolment(t *testing.T) {
	test := newRig(t, nil)
	key := test.enrol(ada)
	answers := map[string]answer{}
	for _, address := range []string{ada, "bea@example.com"} {
		for range accountMisses {
			test.post(test.signins.SignInWithCode, url.Values{
				"address": {address}, "code": {test.wrongCode(key)}})
		}
		other := strings.ToUpper(address[:1]) + address[1:]
		answers[address] = answered(test.post(test.signins.SignInWithCode,
			url.Values{"address": {other}, "code": {test.wrongCode(key)}}))
	}
	if answers[ada] != answers["bea@example.com"] {
		t.Fatalf("enrolled %+v, not enrolled %+v", answers[ada],
			answers["bea@example.com"])
	}
}
