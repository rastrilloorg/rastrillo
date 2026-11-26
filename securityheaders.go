package rastrillo

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/internal/cspallow"
)

// DefaultCSP is the Content-Security-Policy every rastrillo response
// carries unless Options.CSP replaces it or Options.CSPAllow adds to
// it: same-origin scripts, styles, images, fetches and form targets;
// data: images allowed for embedded favicons; framing denied.
//
// No inline styles and no inline scripts: inline styles are how CSS
// exfiltration and UI redress work once markup injection gets through,
// so ui's partials emit none and a grid's --rst-cols comes from the
// app's stylesheet (GitHub #147). Two exceptions pass by hash: pow's
// honeypot (an attribute, so 'unsafe-hashes' too) and auth's fallback
// confirm page's <style>. Spelled out so apps need not link either;
// buildhandler_test.go and auth's confirmcsp_test.go keep them in step.
const DefaultCSP = "default-src 'self'; " +
	"style-src 'self' 'unsafe-hashes' " +
	"'sha256-yJxAE4rjdcckohdlnvecSporPcqS9xOaA4hJxi87LMc=' " +
	"'sha256-81gZUb3tp60YCqhJ0dItBPh5hRQiGbfst4gpwCNPTfI='; " +
	"img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; " +
	"form-action 'self'"

// securityHeaders is the outermost layer of the serving handler: the
// baseline headers a security audit expects on every response, set
// BEFORE anything else runs so that any header an app sets itself —
// in a handler or Options.Wrap middleware — replaces the default, and
// a Del removes it. Framework-owned for the same reason csrf is:
// hand-rolled per-app hygiene is where apps ship gaps.
func securityHeaders(
	csp string, speculate bool, next http.Handler,
) http.Handler {
	if csp == "" {
		csp = DefaultCSP
	}
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		header := response.Header()
		header.Set("Content-Security-Policy", csp)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Unconditional, not gated on request.TLS: behind the CARLOS edge TLS
		// ends before the process, so request.TLS is nil in production and a
		// gate would never send it. That is safe because browsers ignore
		// HSTS received over plain HTTP (RFC 6797 §8.1), so a local
		// `-addr :8080` is unaffected. No includeSubDomains or preload:
		// both commit hosts beyond this app's own, which is the app
		// owner's call — set a wider value in a handler or Options.Wrap.
		header.Set("Strict-Transport-Security", "max-age=31536000")
		// On every response rather than sniffed for HTML: browsers act
		// on it for documents and ignore it elsewhere. A structured-field
		// list of one string, which is the header's form.
		if speculate {
			// Through Path, so under a base the browser fetches this
			// tenant's ruleset, not a path on the hub.
			rules := basepath.Path(request, SpeculationRulesPath)
			header.Set("Speculation-Rules", `"`+rules+`"`)
		}
		next.ServeHTTP(response, request)
	})
}

// allowInCSP returns policy, or DefaultCSP when it is empty, with
// allow's sources added; internal/cspallow says what it refuses.
func allowInCSP(policy string, allow map[string][]string) (string, error) {
	if policy == "" {
		policy = DefaultCSP
	}
	return cspallow.Add(policy, allow)
}
