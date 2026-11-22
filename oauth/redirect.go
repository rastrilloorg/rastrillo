package oauth

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// redirectAllowed reports whether requested is one of the registered
// redirects. Every component must match exactly, so a registered
// https://app.example/cb never admits another path, query or encoding.
// Only the port of a loopback host may differ, because a native agent
// listens on whatever port is free.
func redirectAllowed(registered []string, requested string) bool {
	want, ok := parseRedirect(requested)
	if !ok {
		return false
	}
	for _, candidate := range registered {
		have, ok := parseRedirect(candidate)
		if !ok || have.Scheme != want.Scheme ||
			have.EscapedPath() != want.EscapedPath() ||
			have.RawQuery != want.RawQuery ||
			have.ForceQuery != want.ForceQuery {
			continue
		}
		if loopback(want.Hostname()) {
			if have.Hostname() == want.Hostname() {
				return true
			}
		} else if have.Host == want.Host {
			return true
		}
	}
	return false
}

// parseRedirect parses a redirect URI and applies the rules that need no
// comparison: https, or http to a loopback host, and no fragment or
// userinfo. The approval page shows and links to it, so a quote, an
// angle bracket or white space is refused too.
func parseRedirect(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Host == "" || strings.ContainsAny(raw, `#"<>`) ||
		strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return nil, false
	}
	switch parsed.Scheme {
	case "https":
		return parsed, true
	case "http":
		return parsed, loopback(parsed.Hostname())
	}
	return nil, false
}

func loopback(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// back is where an answer to the client goes: its redirect, and the
// state to return with it.
type back struct {
	redirect, state string
	stateSent       bool
}

// redirect sends the person back to the client with key and value.
func (server *Server) redirect(
	response http.ResponseWriter, request *http.Request, to back, key,
	value string,
) {
	address, err := server.backURL(to, key, value)
	if err != nil {
		server.failed(response, request, "parsing a checked redirect", err)
		return
	}
	http.Redirect(response, request, address, http.StatusSeeOther)
}

// linkBack shows the error page with msg and a link that takes the
// error code back to the client.
func (server *Server) linkBack(
	response http.ResponseWriter, request *http.Request, status int, msg string,
	to back, code string,
) {
	address, err := server.backURL(to, "error", code)
	if err != nil {
		server.failed(response, request, "parsing a checked redirect", err)
		return
	}
	server.render(response, request, status, Page{
		Kind: "error", SiteName: server.cfg.SiteName, Error: msg,
		ReturnURL: address, ReturnHost: hostOf(to.redirect),
	})
}

// backURL is the client's redirect, keeping its own query and adding
// key, state and iss.
func (server *Server) backURL(to back, key, value string) (string, error) {
	parsed, err := url.Parse(to.redirect)
	if err != nil {
		return "", err
	}
	add := url.Values{key: {value}, "iss": {server.issuer}}
	if to.stateSent {
		add.Set("state", to.state)
	}
	if parsed.RawQuery != "" {
		parsed.RawQuery += "&"
	}
	parsed.RawQuery += add.Encode()
	parsed.ForceQuery = false
	return parsed.String(), nil
}
