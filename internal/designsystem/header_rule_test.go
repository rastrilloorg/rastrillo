package designsystem

import (
	"sort"
	"strings"
	"testing"
)

// TestEveryPageWithAHeaderLinksTheOneStylesheetThatRetiresTheRakeLine is
// the completeness argument the browser sweep above cannot afford to
// make by brute force: the tree is six hundred documents, and driving
// all of them to read one border would cost more than it proves.
//
// It is a cheap statement with a real edge. ui's
// TestTheRakeLineIsDeclaredRetiredExactlyOnce says the retirement is
// declared in exactly one stylesheet; this says every document in the
// tree that renders a page header links that stylesheet. Between them,
// and the sweep above showing what that declaration does in a browser,
// "the ::after is gone everywhere" is a claim about the whole tree
// rather than about the pages somebody remembered to drive.
func TestEveryPageWithAHeaderLinksTheOneStylesheetThatRetiresTheRakeLine(t *testing.T) {
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const link = `<link rel="stylesheet" href="` + mountPath + `/tokens.css">`
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	documents, withHeader := 0, 0
	for _, name := range names {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		documents++
		body := string(files[name])
		if !strings.Contains(body, "rst-page-header") {
			continue
		}
		withHeader++
		// Both the page itself and every srcdoc preview inside it are
		// documents, and the srcdoc's own <link> is HTML-escaped inside
		// the attribute. Counting links rather than asserting one covers
		// both without unescaping the page by hand.
		if strings.Count(body, link)+strings.Count(body, htmlEscapeAttr(link)) == 0 {
			t.Errorf("%s renders a page header and links no %s/tokens.css; whatever retires the rake line, it is not reaching this page", name, mountPath)
		}
	}
	if documents == 0 || withHeader == 0 {
		t.Fatalf("walked %d documents, %d of them with a page header; the tree did not render", documents, withHeader)
	}
	t.Logf("%d of %d rendered documents carry a page header, and every one of them links the stylesheet that retires the rake line", withHeader, documents)
}

// htmlEscapeAttr escapes the four characters html/template escapes
// inside a double-quoted attribute value, which is how a <link> looks
// once it is inside an iframe's srcdoc.
func htmlEscapeAttr(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&#34;",
	).Replace(s)
}
