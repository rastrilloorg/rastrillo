package form

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// The English for the two web-address errors, beside their keys for the
// reason money.go's pair is: the strings the catalog translates and the
// strings this package falls back to cannot drift apart.
//
// Credentials get a message of their own because the person did type a
// web address. Telling them to "enter a web address" would send them
// looking for a mistake they did not make.
const (
	urlInvalidKey     = "rastrillo.ui.url_invalid"
	urlCredentialsKey = "rastrillo.ui.url_credentials"

	urlInvalidEN     = "Enter a web address, like example.com."
	urlCredentialsEN = "Remove the username and password from this address."
)

// NormaliseURL reads a web address the way a person types one and
// returns the form to store: an http or https URL with a lowercase host
// and no bare trailing slash. "example.com", "WWW.Example.com/",
// "http://example.com" and "example.com:8443/pricing?plan=team" are all
// accepted; a missing scheme becomes https. Blank is "" and no error,
// so Required stays the caller's decision.
//
// It exists because <input type="url"> is the opposite of forgiving:
// browsers refuse "example.com" for lacking a scheme, which is how most
// people write an address. So field-url is a text input and all the
// forgiveness is here, on the server, where it covers every way a value
// can arrive.
//
// What it refuses is a safety property, not tidiness, because the
// stored value becomes a link: any scheme but http and https
// (javascript:, data:, mailto:), a username or password in the address
// (https://bank.example@evil.example reads as one site and goes to
// another), a host with no dot (localhost, an intranet name), and
// whitespace anywhere inside. Every refusal is an *Error naming its
// catalog key.
//
// The path, query and fragment are kept exactly as typed rather than
// re-encoded, so an address in the reader's own script stays readable:
// url.URL.String would store "münchen.example" as "m%C3%BCnchen.example".
func NormaliseURL(s string) (string, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return "", nil
	}
	invalid := &Error{Key: urlInvalidKey, Msg: urlInvalidEN}
	if strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return "", invalid
	}
	typedScheme := startsWithScheme(v)
	if !typedScheme {
		// "mailto:x" and "javascript:x" have no "://" either. With
		// https:// in front, the part before their colon parses as a
		// host with a non-numeric port and Parse refuses it, or (for
		// mailto) as a username, which the check below refuses.
		v = "https://" + v
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", invalid
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalid
	}
	if u.User != nil {
		// Only an address that arrived with its own scheme can have
		// meant a username. "mailto:ana@example.com" lands here too,
		// and the credentials message would be nonsense to someone who
		// typed an email address.
		if typedScheme {
			return "", &Error{Key: urlCredentialsKey, Msg: urlCredentialsEN}
		}
		return "", invalid
	}
	host := strings.ToLower(u.Hostname())
	if !plausibleHost(host) || !browserSameHost(host) || !validPort(u.Port()) {
		return "", invalid
	}
	hostport := host
	if p := u.Port(); p != "" {
		hostport += ":" + p
	}
	// Everything after the authority, as typed. Parse has already
	// vetted it; slicing it out of the input, rather than rebuilding it
	// from u, is what keeps it from being re-encoded.
	after := v[strings.Index(v, "://")+3:]
	rest := ""
	if i := strings.IndexAny(after, "/?#"); i >= 0 {
		rest = after[i:]
	}
	if rest == "/" {
		rest = ""
	}
	return scheme + "://" + hostport + rest, nil
}

// startsWithScheme reports whether v opens with "scheme://". Looking
// for "://" anywhere would misread "example.com/login?next=https://x" as
// schemed, and Parse would then refuse an address a person reasonably
// typed.
func startsWithScheme(v string) bool {
	i := strings.Index(v, "://")
	if i <= 0 {
		return false
	}
	for j, r := range v[:i] {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (j == 0 || !(r >= '0' && r <= '9' || r == '+' || r == '.' || r == '-')) {
			return false
		}
	}
	return true
}

// plausibleHost is the "is this a web address at all" test: a dot
// somewhere inside (which is what rules out localhost and a bare word,
// the usual slip being a company name typed into the wrong field), no
// empty label, and letters, digits and hyphens only, in any script. An
// IP literal in brackets fails on its colons, deliberately: a link to
// [::1] is not something an app user means to store.
func plausibleHost(host string) bool {
	if host == "" || !strings.Contains(host, ".") ||
		strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") ||
		strings.Contains(host, "..") {
		return false
	}
	for _, r := range host {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// browserSameHost reports whether a browser will go to the host as
// written. A browser reads any host whose last label is a number as an
// IPv4 address, in octal where a part has a leading zero and in hex
// after 0x: "0127.0.0.1" goes to 87.0.0.1, and "0177.0.0.1" to
// loopback. Stored as typed, that is a link whose text names one
// machine and whose target is another, so a numeric host must be a
// dotted-decimal address with nothing a browser would re-read.
func browserSameHost(host string) bool {
	last := host[strings.LastIndex(host, ".")+1:]
	hex := len(last) >= 2 && last[0] == '0' && (last[1] == 'x' || last[1] == 'X')
	digits := last
	if hex {
		digits = last[2:]
	}
	numeric := true
	for _, r := range digits {
		isDigit := r >= '0' && r <= '9'
		isHex := isDigit || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if (hex && !isHex) || (!hex && !isDigit) {
			numeric = false
			break
		}
	}
	if !numeric || (!hex && last == "") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Is4() && addr.String() == host
}

// validPort is net/url's port check plus the range: Parse takes any run
// of digits, and a browser refuses a link to port 65536, so accepting
// one would store an address that cannot be followed.
func validPort(p string) bool {
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && len(p) <= 5 && n <= 65535
}

// DisplayURL is how a stored address reads on screen: no scheme and no
// trailing slash, "brightwater.example/pricing" rather than
// "https://brightwater.example/pricing/". The scheme is noise to a
// reader, and a list of sites all starting "https://" is harder to
// scan. It is display only: it never makes a value safe to link, which
// is SafeHref's job.
func DisplayURL(s string) string {
	v := strings.TrimSpace(s)
	for _, scheme := range []string{"https://", "http://"} {
		if len(v) >= len(scheme) && strings.EqualFold(v[:len(scheme)], scheme) {
			v = v[len(scheme):]
			break
		}
	}
	return strings.TrimSuffix(v, "/")
}

// SafeHref is s as a link target, or "" when s is not an absolute http
// or https URL with a host and no credentials. The empty string is the
// point: a template writes {{with safeHref .Site}}<a href="{{.}}">, and
// a value that predates validation, or reached the database some other
// way, renders as no link at all rather than as a javascript: URL or a
// mailto: that html/template would have let through.
func SafeHref(s string) string {
	v := strings.TrimSpace(s)
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil ||
		!browserSameHost(strings.ToLower(u.Hostname())) || !validPort(u.Port()) {
		return ""
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	return v
}

// URLKey is an address's identity for spotting duplicates: host without
// "www.", port if any, and path, lowercased, with no scheme and no
// trailing slash. "https://www.Brightwater.example/" and
// "brightwater.example" are the same key, so one site typed three ways
// is one value. It is deliberately looser than NormaliseURL, which keeps
// what was typed; store the normalised form and compare the keys.
//
// A value that does not normalise is keyed by its trimmed, lowercased
// text, so comparing two bad values still behaves.
func URLKey(s string) string {
	n, err := NormaliseURL(s)
	if err != nil || n == "" {
		return strings.ToLower(strings.TrimSpace(s))
	}
	u, err := url.Parse(n)
	if err != nil {
		return strings.ToLower(n)
	}
	key := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if p := u.Port(); p != "" {
		key += ":" + p
	}
	return strings.TrimSuffix(key+strings.ToLower(u.EscapedPath()), "/")
}
