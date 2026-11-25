module amadan.net/rastrillo/rastrillo/bench/agent

go 1.26.0

require (
	github.com/chromedp/cdproto v0.158.6
	github.com/chromedp/chromedp v0.21.0
	github.com/mibk/dupl v1.1.0
	github.com/uudashr/gocognit v1.3.0
	golang.org/x/mod v0.43.0
	golang.org/x/tools v0.53.0
	mvdan.cc/sh/v3 v3.13.1
)

require golang.org/x/sync v0.25.0 // indirect

require (
	amadan.net/rastrillo/rastrillo v0.0.0
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/ws v1.5.0 // indirect
)

replace amadan.net/rastrillo/rastrillo => ../..

// The root module raises chromedp and cdproto to v0.21; internal/measure/browser.go is not ported to it yet.
replace github.com/chromedp/chromedp v0.21.0 => github.com/chromedp/chromedp v0.14.2

replace github.com/chromedp/cdproto v0.158.6 => github.com/chromedp/cdproto v0.0.0-20250724212937-08a3db8b4327
