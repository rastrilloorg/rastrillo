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
	link := `<link rel="stylesheet" href="` + assetHref("tokens.css") + `">`
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	documents, withHeader, framesChecked := 0, 0, 0
	for _, name := range names {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		body := string(files[name])
		documents++
		if !strings.Contains(body, "rst-page-header") {
			continue
		}
		withHeader++
		// A preview is a separate document; the page that frames it
		// retires nothing on its behalf.
		if isPreviewDoc(body) {
			framesChecked++
		}
		if !strings.Contains(body, link) {
			t.Errorf("%s renders a page header and links no %s; whatever retires the rake line, it is not reaching this document", name, assetHref("tokens.css"))
		}
	}
	if documents == 0 || withHeader == 0 || framesChecked == 0 {
		t.Fatalf("walked %d documents, %d of them with a page header, %d of those preview frames; the tree did not render, or the previews stopped being documents of their own", documents, withHeader, framesChecked)
	}
	t.Logf("%d of %d rendered documents carry a page header — %d of them preview frames judged on their own bytes — and every one links the stylesheet that retires the rake line", withHeader, documents, framesChecked)
}
