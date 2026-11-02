package vendored

import (
	"bytes"
	"path"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/js"
)

// minifier keeps the scripts' variable names, so the served file
// differs from the commented source by comments and whitespace as
// nearly as the minifier allows, and a stack trace still names the
// source's variables.
var minifier = func() *minify.M {
	minifier := minify.New()
	minifier.AddFunc("text/css", css.Minify)
	minifier.Add("text/javascript", &js.Minifier{KeepVarNames: true})
	return minifier
}()

// mediaTypes maps the extensions Minify handles to the minifier's
// media types.
var mediaTypes = map[string]string{
	".css": "text/css",
	".js":  "text/javascript",
}

// Minify returns body with its comments removed and its whitespace
// collapsed, by name's extension. A file that is neither CSS nor
// JavaScript comes back unchanged.
func Minify(name string, body []byte) ([]byte, error) {
	mediaType, ok := mediaTypes[path.Ext(name)]
	if !ok {
		return body, nil
	}
	var out bytes.Buffer
	err := minifier.Minify(mediaType, &out, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
