package blobs

// Minimal AWS Signature Version 4 signer — just enough to talk to S3.
// Extracted from messenger's home/sigv4.go (the family precedent for
// hand-rolling this): the store needs a handful of operations, and an
// AWS SDK would multiply the dependency surface for one HMAC chain.
// Pinned offline against the official AWS worked examples in
// sigv4_test.go.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// emptyPayloadHash is sha256("") — the x-amz-content-sha256 for bodyless
// requests (GET/HEAD/DELETE).
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// awsSignV4 computes the SigV4 signature for req and sets its Authorization
// header. The caller must already have set x-amz-date (matching t) and
// x-amz-content-sha256 (the hex payload hash — signed, so the body can't be
// swapped in flight). Signed headers are host plus every x-amz-* header plus
// range/content-type/content-md5 when present; conditional headers (If-Match
// etc.) ride unsigned, which S3 permits — only x-amz-* headers MUST be signed.
func awsSignV4(req *http.Request, accessKey, secretKey, region, service string, t time.Time) {
	amzDate := t.UTC().Format("20060102T150405Z")
	scope := t.UTC().Format("20060102") + "/" + region + "/" + service + "/aws4_request"

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	type hdr struct{ name, value string }
	signed := []hdr{{"host", host}}
	for name, vals := range req.Header {
		ln := strings.ToLower(name)
		if strings.HasPrefix(ln, "x-amz-") || ln == "range" || ln == "content-type" || ln == "content-md5" {
			signed = append(signed, hdr{ln, strings.TrimSpace(strings.Join(vals, ","))})
		}
	}
	sort.Slice(signed, func(i, j int) bool { return signed[i].name < signed[j].name })
	names := make([]string, len(signed))
	var canonHdrs strings.Builder
	for i, h := range signed {
		canonHdrs.WriteString(h.name)
		canonHdrs.WriteByte(':')
		canonHdrs.WriteString(h.value)
		canonHdrs.WriteByte('\n')
		names[i] = h.name
	}
	signedHeaders := strings.Join(names, ";")

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := req.Method + "\n" +
		path + "\n" +
		canonicalQuery(req.URL.Query()) + "\n" +
		canonHdrs.String() + "\n" +
		signedHeaders + "\n" +
		req.Header.Get("x-amz-content-sha256")

	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256([]byte(canonical))

	// The HMAC chain: secret → date → region → service → "aws4_request".
	key := hmacSHA256([]byte("AWS4"+secretKey), t.UTC().Format("20060102"))
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
			",SignedHeaders="+signedHeaders+",Signature="+sig)
}

// canonicalQuery renders url.Values in SigV4 canonical form: keys sorted,
// values sorted within a key, strict RFC 3986 escaping (%20 for space, never
// '+'), and flag parameters ("?versions") rendered with a trailing '='.
func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(q))
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, awsURIEncode(k, true)+"="+awsURIEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// awsURIEncode percent-encodes per the SigV4 rules: unreserved characters
// (A-Za-z0-9, '-', '.', '_', '~') pass through; '/' passes through only in
// paths (encodeSlash=false); everything else — including '+' and space —
// becomes %XX with uppercase hex.
func awsURIEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}
