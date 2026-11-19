//go:build browser

package ui

import (
	"context"
	"fmt"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

func TestSkippedShellTransitionsDoNotReportUncaughtErrors(t *testing.T) {
	for _, event := range []string{"pagereveal", "pageswap"} {
		t.Run(event, func(t *testing.T) {
			rig := harness.New(t, shellSite(t))
			ctx := rig.Context()
			script := fmt.Sprintf(`addEventListener(%q, event => {
  const transition = event.viewTransition;
  if (!transition) return;
  sessionStorage.setItem("skipped-transition", event.type);
  void transition.ready;
  void transition.finished;
  void transition.updateCallbackDone;
  transition.skipTransition();
});`, event)
			rig.Run(chromedp.EmulateViewport(390, 844),
				send(emulation.SetEmulatedMedia,
					emulation.SetEmulatedMediaParams{
						Features: []*emulation.MediaFeature{{
							Name:  "prefers-reduced-motion",
							Value: "no-preference",
						}},
					}),
				chromedp.Func(func(ctx context.Context,
					target *chromedp.Target,
				) error {
					_, err := cdp.Call(ctx, target,
						cdppage.AddScriptToEvaluateOnNewDocument,
						cdppage.AddScriptToEvaluateOnNewDocumentParams{
							Source: script,
						})
					return err
				}))
			visit(t, ctx, rig.Origin+"/")
			revealed(t, ctx)
			follow(t, ctx, "#nav-invoices",
				`location.pathname === "/invoices"`)
			revealed(t, ctx)
			settled(t, ctx)
			if got := harness.Evaluate[string](rig,
				`sessionStorage.getItem("skipped-transition") || ""`,
			); got != event {
				t.Fatalf("no %s transition was skipped: %q", event, got)
			}
			if event == "pageswap" {
				// The outgoing page can freeze before reporting its rejection.
				// Restore it, then load a fresh DOM for Screen's selector wait.
				back(t, ctx, `location.pathname === "/"`)
				revealed(t, ctx)
				settled(t, ctx)
				visit(t, ctx, rig.Origin+"/orders")
				revealed(t, ctx)
				settled(t, ctx)
			}
			rig.Screen("body", event+" transition skipped")
		})
	}
}
