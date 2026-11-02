package rastrillo

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// serveVendored writes a vendored file's minified bytes, gzipped when
// the client accepts it. Each encoding has its own ETag and the answer
// varies on Accept-Encoding, so no cache hands the gzip to a client
// that refused it.
func (assets *Assets) serveVendored(
	writer http.ResponseWriter,
	request *http.Request,
	name, hashInURL string,
) bool {
	entry, err := assets.lookup(name)
	if err != nil {
		return false
	}
	header := writer.Header()
	header.Set("Cache-Control", cachePolicy(entry.hash, hashInURL))
	header.Add("Vary", "Accept-Encoding")
	body, etag := entry.served, `"`+entry.hash+`"`
	if acceptsGzip(request) {
		body, etag = entry.gzipped, `"`+entry.hash+`-gzip"`
		header.Set("Content-Encoding", "gzip")
	}
	header.Set("ETag", etag)
	http.ServeContent(
		writer, request, name, entry.mtime, bytes.NewReader(body),
	)
	return true
}

// servedForms returns the bytes a vendored file is served as and their
// gzip. A file the minifier refuses, such as an app's edit that does
// not parse, is served as written rather than as an error, and so is
// one it empties, such as a file of empty rules: a stylesheet that
// arrives empty looks like a broken deploy, not a smaller one.
func servedForms(name string, body []byte) (served, gzipped []byte) {
	served, err := vendored.Minify(name, body)
	if err != nil || len(served) == 0 {
		served = body
	}
	var compressed bytes.Buffer
	zipper, _ := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	zipper.Write(served)
	zipper.Close()
	return served, compressed.Bytes()
}

// acceptsGzip reports whether the request's Accept-Encoding allows
// gzip: a gzip coding, or failing that a *, whose q is not 0.
func acceptsGzip(request *http.Request) bool {
	star := false
	for _, field := range request.Header.Values("Accept-Encoding") {
		for part := range strings.SplitSeq(field, ",") {
			coding, parameters, _ := strings.Cut(part, ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			switch coding {
			case "gzip":
				return !refused(parameters)
			case "*":
				star = !refused(parameters)
			}
		}
	}
	return star
}

// refused reports whether a coding's parameters carry q=0.
func refused(parameters string) bool {
	for parameter := range strings.SplitSeq(parameters, ";") {
		key, value, _ := strings.Cut(strings.TrimSpace(parameter), "=")
		if strings.EqualFold(key, "q") {
			quality, err := strconv.ParseFloat(value, 64)
			return err == nil && quality == 0
		}
	}
	return false
}
