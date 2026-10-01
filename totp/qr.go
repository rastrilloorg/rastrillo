package totp

import (
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// qrSVG renders text as a QR code in inline SVG: one path of unit
// squares over a viewBox the size of the matrix (quiet zone included),
// drawn in currentColor so it follows the page's ink in either theme.
// Inline rather than a data: PNG so it needs no img-src allowance and
// scales without blur; an SVG is also the one image form a page can
// carry with a strict same-origin CSP.
func qrSVG(text, label string) (string, error) {
	q, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bits := q.Bitmap()
	n := len(bits)
	var d strings.Builder
	for y, row := range bits {
		for x, on := range row {
			if on {
				d.WriteString("M" + itoa(x) + " " + itoa(y) + "h1v1h-1z")
			}
		}
	}
	size := itoa(n)
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + size + ` ` + size +
		`" role="img" aria-label="` + escapeAttr(label) + `" shape-rendering="crispEdges">` +
		`<path fill="currentColor" d="` + d.String() + `"/></svg>`, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func escapeAttr(s string) string {
	r := strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;", `>`, "&gt;")
	return r.Replace(s)
}
