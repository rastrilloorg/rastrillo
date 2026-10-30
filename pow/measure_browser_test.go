//go:build browser

package pow_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// TestMeasureSolveTimes times the shipped solver in Chromium with the
// CPU throttled, to choose a difficulty from measurement rather than a
// guess. Solve time is geometric, so it reports p95 and p99, not the
// mean: calibrating on the mean ships a form that hangs for one visitor
// in a hundred. Opt-in: it takes minutes.
//
//	POW_MEASURE=1 TMPDIR=/var/tmp go test -tags browser -run Measure -v ./pow/
func TestMeasureSolveTimes(t *testing.T) {
	if os.Getenv("POW_MEASURE") == "" {
		t.Skip("set POW_MEASURE=1 to measure")
	}
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	for _, rate := range []float64{1, 4, 6} {
		rig.Run(chromedp.Func(
			func(ctx context.Context, target *chromedp.Target) error {
				_, err := cdp.Call(ctx, target, emulation.SetCPUThrottlingRate,
					emulation.SetCPUThrottlingRateParams{Rate: rate})
				return err
			}))
		for bits := 12; bits <= 18; bits++ {
			samples := 60
			if bits >= 17 {
				samples = 30
			}
			var ms []float64
			for i := 0; i < samples; i++ {
				nonce := fmt.Sprintf("%032x",
					time.Now().UnixNano()+int64(i))
				solveScript := fmt.Sprintf(`(async () => {
					const m = await import("/pow/powcore.js");
					const t0 = performance.now();
					m.solve(%q, "", %d, null);
					return performance.now() - t0;
				})()`, nonce, bits)
				took := harness.Evaluate[float64](rig, solveScript,
					chromedp.EvalAwaitPromise)
				ms = append(ms, took)
			}
			sort.Float64s(ms)
			q := func(p float64) float64 { return ms[int(p*float64(len(ms)-1))] }
			t.Logf("throttle %gx  %2d bits  p50 %6.0fms  p95 %6.0fms  p99 %6.0fms", rate, bits, q(0.5), q(0.95), q(0.99))
		}
	}
}
