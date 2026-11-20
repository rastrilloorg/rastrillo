package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// The sections task asks whether an app's colour-scheme control sits
// in the account card, as the shipped layouts put it. A line-by-line
// pattern cannot answer that: an opening tag may span lines and a
// closing tag may carry a space before its >. So templates are read by
// an HTML tokenizer, with their Go template actions made opaque first,
// because an action's own quotes inside an attribute value would end
// that value early.

var (
	templateCommentRE = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)
	templateActionRE  = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	schemeCallRE      = regexp.MustCompile(`^\{\{-?\s*template\s+"scheme-menu"`)
)

// schemeCall stands in for a scheme-menu call once actions are opaque.
const schemeCall = "skilleval-scheme-menu-call"

// cardAttributes mark the account card: in a bar, and in the sidebar's
// rail foot.
var cardAttributes = []string{"rst-shell-account", "rst-shell-profile"}

// voidElements never hold content, so they open nothing to close.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"source": true, "track": true, "wbr": true,
}

// schemeInCard reports whether a template calls scheme-menu, or writes
// an [rst-scheme] element, inside an open element carrying a card
// attribute.
func schemeInCard(template string) bool {
	template = templateCommentRE.ReplaceAllString(template, " ")
	template = templateActionRE.ReplaceAllStringFunc(template,
		func(action string) string {
			if schemeCallRE.MatchString(action) {
				return " " + schemeCall + " "
			}
			return " "
		})
	var cards cardDepth
	tokens := html.NewTokenizer(strings.NewReader(template))
	for {
		kind := tokens.Next()
		switch kind {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if cards.inside() && hasAttribute(token, "rst-scheme") {
				return true
			}
			if kind == html.StartTagToken && !voidElements[token.Data] {
				cards.open(token.Data, hasAttribute(token, cardAttributes...))
			}
		case html.EndTagToken:
			cards.close(tokens.Token().Data)
		case html.TextToken:
			if cards.inside() &&
				strings.Contains(string(tokens.Text()), schemeCall) {
				return true
			}
		}
	}
}

// cardDepth is the stack of open elements, and how many are cards.
type cardDepth struct {
	elements []string
	cards    []bool
	count    int
}

func (depth *cardDepth) inside() bool { return depth.count > 0 }

func (depth *cardDepth) open(element string, card bool) {
	depth.elements = append(depth.elements, element)
	depth.cards = append(depth.cards, card)
	if card {
		depth.count++
	}
}

// close ends the nearest open element of that name and everything
// opened inside it, as a browser does with an unclosed child; an end
// tag that matches nothing open is ignored.
func (depth *cardDepth) close(element string) {
	for at := len(depth.elements) - 1; at >= 0; at-- {
		if depth.elements[at] != element {
			continue
		}
		for _, card := range depth.cards[at:] {
			if card {
				depth.count--
			}
		}
		depth.elements, depth.cards = depth.elements[:at], depth.cards[:at]
		return
	}
}

func hasAttribute(token html.Token, names ...string) bool {
	for _, attribute := range token.Attr {
		for _, name := range names {
			if attribute.Key == name {
				return true
			}
		}
	}
	return false
}

// errFoundInCard stops the walk at the first template that answers yes.
var errFoundInCard = errors.New("found")

// schemeInCardUnder reports whether any .html or .htm template under
// root, outside gen/, has the scheme control in the account card.
func schemeInCardUnder(root string) (bool, error) {
	err := filepath.WalkDir(root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "gen" {
				return filepath.SkipDir
			}
			extension := filepath.Ext(path)
			if entry.IsDir() || extension != ".html" && extension != ".htm" {
				return nil
			}
			text, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if schemeInCard(string(text)) {
				return errFoundInCard
			}
			return nil
		})
	if errors.Is(err, errFoundInCard) {
		return true, nil
	}
	return false, err
}

// init answers `skilleval incard <dir>` before anything else runs, in
// the harness's binary and in this package's test binary alike, so a
// check can call either back as SKILLEVAL_BIN.
func init() {
	if len(os.Args) > 1 && os.Args[1] == "incard" {
		os.Exit(runInCard(os.Args[2:]))
	}
}

// runInCard is `skilleval incard <dir>`, which sections.check.sh calls:
// exit 0 when the control is in the card, 1 when it is not, 2 when the
// tree could not be read.
func runInCard(arguments []string) int {
	if len(arguments) != 1 {
		os.Stderr.WriteString("usage: skilleval incard <dir>\n")
		return 2
	}
	found, err := schemeInCardUnder(arguments[0])
	if err != nil {
		os.Stderr.WriteString("incard: " + err.Error() + "\n")
		return 2
	}
	if found {
		return 0
	}
	return 1
}
