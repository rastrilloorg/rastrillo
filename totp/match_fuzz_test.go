package totp

import (
	"bytes"
	"testing"
	"time"
)

// FuzzMatch feeds the code field a person, or a guesser, submits. A hit
// must name a step inside the window, past the last one spent, whose
// code is the submission once its spaces and dashes are gone; anything
// laxer widens a million-code space or lets a spent code back in.
func FuzzMatch(f *testing.F) {
	secret := bytes.Repeat([]byte{8}, 20)
	now := time.Unix(1_700_000_000, 0)
	current := code(secret, counter(now))
	f.Add(current, int64(0))
	f.Add(current[:3]+" "+current[3:], int64(0))
	f.Add(current[:3]+"-"+current[3:], int64(0))
	f.Add(current, counter(now))
	f.Add(current, counter(now)-1)
	f.Add(code(secret, counter(now)+1), int64(0))
	f.Add(code(secret, counter(now)+2), int64(0))
	f.Add("+"+current[1:], int64(0))
	f.Add("١٢٣٤٥٦", int64(0))
	f.Add("", int64(-1))
	f.Fuzz(func(t *testing.T, submitted string, spent int64) {
		step := match(secret, submitted, now, spent)
		if step == -1 {
			return
		}
		window := counter(now)
		if step < window-skew || step > window+skew || step <= spent {
			t.Fatalf("%q matched step %d; window %d±%d, spent %d",
				submitted, step, window, skew, spent)
		}
		if normalize(submitted) != code(secret, step) {
			t.Fatalf("%q matched step %d, whose code is %q",
				submitted, step, code(secret, step))
		}
	})
}
