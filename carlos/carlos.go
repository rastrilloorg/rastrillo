// Package carlos is the app side of the CARLOS platform's scheduled-work
// contract: receiving a tick, and registering a one-shot timer.
//
// A CARLOS instance hibernates, so an in-process timer is not a
// scheduler — the process is not there when it fires. The platform owns
// the clock instead. A recurring schedule is declared once from outside
// the app (`carlos schedule set -name sync -every 6h -path /jobs/sync`),
// and when it comes due the platform wakes the instance and POSTs to
// that path; a one-shot timer is registered by the app itself over the
// instance's control socket and delivered the same way. Either way the
// app's whole job is an ordinary handler that answers a POST.
//
// Two things follow, and both are why this package exists rather than
// each app hand-rolling them:
//
// A tick arrives on a public route. Nothing about it is magic beyond the
// headers, so a tick nobody authenticated is an internet request to that
// path — [Tick] is the constant-time bearer check that tells them apart.
//
// Delivery is at-least-once. A handler that runs past the platform's
// timeout, a box that crashes mid-tick, a deploy that cuts the instance
// out from under a long job: each ends with the occurrence delivered
// again. [TickOccurrence] hands back the key that is stable across those
// retries, so an app that records what it has finished can be idempotent
// per occurrence instead of per wall clock.
//
// Off CARLOS — a dev laptop, a test — nothing here panics or blocks:
// [Tick] is false with no token in the environment (fail closed, never
// open), and [ScheduleAt] returns [ErrNotOnCarlos].
//
// The contract itself lives in carlosframework/platform, spec
// 2026-08-23-scheduled-work-design.md §7.
package carlos

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
)

// tokenEnv holds the instance-local secret the platform mints and puts
// in the process environment: the bearer a tick presents, and the one
// the control socket demands.
const tokenEnv = "CARLOS_ADMIN_TOKEN"

// Tick reports whether r carries the instance's own $CARLOS_ADMIN_TOKEN
// as its bearer — i.e. whether it came from the platform's scheduler
// rather than from the internet. Guard every handler a schedule points
// at with it:
//
//	func handleSync(w http.ResponseWriter, r *http.Request) {
//		if !carlos.Tick(r) {
//			http.Error(w, "forbidden", http.StatusForbidden)
//			return
//		}
//		// Work HERE, synchronously: the platform holds the instance
//		// awake for as long as this request is open and starts the
//		// idle clock when it returns. 202-and-a-goroutine means the
//		// instance may hibernate mid-job.
//		if err := syncer.RunOnce(r.Context()); err != nil {
//			http.Error(w, err.Error(), http.StatusInternalServerError) // 5xx: retry
//			return
//		}
//		w.WriteHeader(http.StatusNoContent)
//	}
//
// No token in the environment means nobody can authenticate, so Tick is
// false: an app that has not been given the secret must not accept a
// tick on the X-Carlos-Schedule headers alone, which any caller can set.
//
// Tick checks the bearer and nothing else, deliberately. The same token
// is how an internal "Sync now" path reaches the same handler, and
// requiring the schedule headers would refuse it for no gain — a caller
// who has the token can set headers too.
func Tick(r *http.Request) bool {
	token := os.Getenv(tokenEnv)
	if token == "" {
		return false
	}
	// sha256 both sides before comparing: ConstantTimeCompare is only
	// constant-time for equal lengths, and it returns 0 immediately on a
	// length mismatch — which leaks the token's length to a caller who
	// can time it. Hashing makes both operands 32 bytes always.
	got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	want := sha256.Sum256([]byte("Bearer " + token))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// TickOccurrence returns the occurrence key of an authentic tick: the
// X-Carlos-Schedule-At header, the instant this delivery is *for*, as
// unix seconds. It is false for anything Tick refuses, and for a tick
// with no such header.
//
// The value is what makes an app idempotent, because it is stable where
// the wall clock is not. Every retry of one failed occurrence carries
// the value the first attempt did, and so does a redelivery after a box
// crash. Record it and skip work you have already done:
//
//	occ, ok := carlos.TickOccurrence(r)
//	if !ok { ... }
//	if done(occ) { w.WriteHeader(http.StatusNoContent); return }
//
// Treat it as an opaque string. It is unique per occurrence of one
// schedule, not across schedules: if a single handler serves more than
// one, key on the X-Carlos-Schedule name header alongside it.
func TickOccurrence(r *http.Request) (string, bool) {
	if !Tick(r) {
		return "", false
	}
	at := r.Header.Get("X-Carlos-Schedule-At")
	return at, at != ""
}
