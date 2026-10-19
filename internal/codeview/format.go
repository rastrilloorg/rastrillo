package codeview

import (
	"regexp"
	"strings"
)

// blockElements are the elements whose boxes are blocks: whitespace
// between two tags where one of these is involved renders as nothing,
// so a line break there moves no pixel. Everything else (a, span,
// button, label, input, svg) is inline or inline-block, where a space
// is a space a reader sees, and is broken only where one of the rules
// in layout says the space cannot show.
var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
	"dialog": true, "dd": true, "div": true, "dl": true, "dt": true, "fieldset": true,
	"figcaption": true, "figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
	"legend": true, "li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true,
	"search": true, "section": true, "summary": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
	// Inside a select, which listElements covers: an option and a
	// group are drawn by the control, not laid out as text.
	"optgroup": true, "option": true,
}

// listElements hold a list the control draws (a select's options, a
// datalist's suggestions), so nothing between their children renders
// and each child can have a line. The element itself is inline-level:
// a select sits in a sentence like a word, and the space after it
// shows, so it is not a block and is never broken out of its line.
var listElements = map[string]bool{"select": true, "datalist": true, "optgroup": true}

// rawElements keep every byte inside them. A <pre> or <textarea>
// renders its whitespace as written, script and style are not markup,
// an svg's paths are drawing, not structure a reader reads, and a
// template's content is not rendered at all.
var rawElements = map[string]bool{"pre": true, "textarea": true, "svg": true, "script": true, "style": true, "template": true}

// preservingStyle matches an inline style that makes whitespace render
// as written: white-space pre, pre-wrap, pre-line or break-spaces, or the
// longhand that says the same. Under any of them a line break and an
// indent are text a reader sees, so the element's whole subtree is left
// as it is, the way a <pre> is. nowrap is not here: it collapses spaces
// exactly as normal does and only stops the wrapping.
var preservingStyle = regexp.MustCompile(`(?i)white-space(?:-collapse)?\s*:\s*(?:pre|pre-wrap|pre-line|break-spaces|preserve|preserve-breaks|preserve-spaces)\b`)

// preservingAttrs are the attributes whose rules in tokens.css preserve
// whitespace. None do today; one that comes to must be named here, or
// the formatter will lay its content out as if spaces collapsed.
var preservingAttrs = map[string]bool{}

// preserves says whether the element opened at p keeps its content's
// whitespace as written, so its subtree must not be reformatted.
func preserves(p piece) bool {
	if rawElements[p.name] {
		return true
	}
	attrs := attrsOf(p.text)
	for a := range attrs {
		if preservingAttrs[a] {
			return true
		}
	}
	return preservingStyle.MatchString(attrs["style"])
}

// silentElements render nothing, so a space beside one is only seen if
// something visible is beside it too. layout treats them as absent when
// it asks whether a gap is at the edge of a line.
var silentElements = map[string]bool{"script": true, "style": true, "template": true, "link": true, "meta": true}

// ── Flex and grid containers ─────────────────────────────────────────
//
// Whitespace between the children of a flex or grid container renders
// as nothing: a run of only whitespace makes no item, and a run of text
// is an item of its own whose leading and trailing spaces fall at the
// edges of its own line. So inside one, every child can have a line of
// its own. This is the list of the containers rastrillo's tokens.css
// lays out that way, by the attribute that does it, and it is a list
// rather than a guess on purpose: a container wrongly on it would move
// a reader's text, and one missing from it only keeps a line long.
//
// An entry is here only when the element is flex or grid at every
// width and in every context the rule names. The shell chrome that
// switches between none, contents and flex across breakpoints is not,
// and stays inline. ui's tokens.css is the authority; the gallery's
// formatting drive (TestTheHTMLTabRendersExactlyLikeThePreview) renders
// every sample both ways and fails on any box that moves.

// flexAttrs are the attributes that make their element a block-level
// flex or grid container.
var flexAttrs = map[string]bool{
	"rst-page-header": true, "rst-lbar": true, "rst-search": true, "rst-row": true, "rst-row-lead": true,
	"rst-pagination": true, "rst-stats": true, "rst-stat": true, "rst-person-av": true, "rst-callout": true,
	"rst-detail": true, "rst-box-head": true, "rst-lrow": true, "rst-choice-cards": true, "rst-seg-tabs": true,
	"rst-field-row": true, "rst-form-bar": true, "rst-form-actions": true, "rst-form": true, "rst-form-foot": true,
	"rst-modal-overlay": true, "rst-modal-panel": true, "rst-bulkbar": true, "rst-shell-nav": true,
	"rst-shell-rail-foot": true, "rst-signin": true, "rst-signin-brand": true, "rst-signin-door": true,
	"rst-signin-form": true, "rst-signin-passkey": true, "rst-signin-or": true, "rst-signin-providers": true,
	"rst-stage": true, "rst-error-cta": true, "rst-dtp-pick": true, "rst-dtp-row": true, "rst-cal-head": true,
	"rst-cal-nav": true, "rst-media": true, "rst-media-name": true,
}

// inlineFlexAttrs are the attributes that make their element an
// inline-level flex container: its children may each have a line, but
// the element itself sits in its parent's line like a word.
var inlineFlexAttrs = map[string]bool{
	"rst-btn": true, "rst-search-clear": true, "rst-status": true, "rst-meter": true, "rst-person": true,
	"rst-callout-ic": true, "rst-caret": true, "rst-ftok": true, "rst-switch": true, "rst-help": true,
	"rst-bulkbar-close": true, "rst-selbox": true, "rst-shell-avatar": true, "rst-row-action": true,
	"rst-bulkbar-escalate": true, "rst-modal-close": true, "rst-shell-brand": true,
}

// flexInContext are the containers tokens.css makes flex or grid only
// inside another: a dropdown's summary, the modal's rail, a choice
// card. ancestor is an attribute on the parent, or on any ancestor when
// deep is set. inline marks the inline-level ones.
var flexInContext = []struct {
	tag, attr, ancestor string
	deep, inline        bool
}{
	{tag: "summary", ancestor: "rst-dropdown"},
	{tag: "summary", ancestor: "rst-row-menu"},
	{tag: "summary", ancestor: "rst-shell-profile", inline: true},
	{tag: "nav", ancestor: "rst-modal-panel"},
	{tag: "label", ancestor: "rst-choice-cards", deep: true},
	{attr: "rst-tblock-head", ancestor: "rst-tblock"},
	{attr: "rst-page", ancestor: "rst-stage"},
	{attr: "rst-shell-bar", ancestor: "rst-shell-topbar", deep: true},
	{attr: "rst-shell-bar", ancestor: "rst-shell-console", deep: true},
	{tag: "a", ancestor: "rst-ftok", deep: true, inline: true},
	{tag: "a", ancestor: "rst-back-nav", deep: true, inline: true},
	{tag: "a", ancestor: "rst-signin-providers", inline: true},
}

// blockAttrs are the attributes tokens.css makes display: block at
// every width, on elements that are inline by default: a field's label
// and its messages, a person's name and address, a choice card's two
// lines.
var blockAttrs = map[string]bool{
	"rst-field-label": true, "rst-field-help": true, "rst-field-error": true, "rst-field-hint": true,
	"rst-person-name": true, "rst-person-email": true, "rst-choice-title": true, "rst-choice-desc": true,
	"rst-tblock-title": true, "rst-tblock-desc": true, "rst-row-sub": true, "rst-job-bar": true,
}

// blockInContext are the elements tokens.css makes blocks only inside
// another: the links and buttons of a dropdown's or a row menu's panel.
var blockInContext = []struct{ tag, ancestor string }{
	{"a", "rst-dropdown-menu"}, {"button", "rst-dropdown-menu"},
	{"a", "rst-row-menu-panel"}, {"button", "rst-row-menu-panel"},
}

// elem is one open element and what its box does to whitespace.
type elem struct {
	name  string
	attrs map[string]string
	// flex: its children are flex or grid items.
	flex bool
	// ownLines: its content starts a line of its own, so a space at the
	// start of that content is at the edge of a line (a block, any flex
	// or grid container, a button).
	ownLines bool
	// blockLevel: the element is not part of its parent's line, so
	// whatever follows it starts a new one.
	blockLevel bool
	// elements: it has an element among its children. A flex item's
	// text gets a line of its own only beside one: a button holding a
	// label and nothing else reads best as it is, <button>Save</button>,
	// and a spinner beside that label is two children to list.
	elements bool
	// broken: a line break was written inside it, so its end tag goes on
	// a line of its own too. One that opened and closed on the same line
	// keeps its end there: <small>AB3PX · <bdi>…</bdi></small>.
	broken bool
}

// describeElem works out what an element opened at p does, given the
// elements it sits in.
func describeElem(p piece, stack []elem) elem {
	e := elem{name: p.name, attrs: attrsOf(p.text)}
	e.blockLevel = blockElements[p.name]
	inline := false
	for _, c := range blockInContext {
		if c.tag == p.name && len(stack) > 0 && has(stack[len(stack)-1].attrs, c.ancestor) {
			e.blockLevel = true
		}
	}
	for a := range e.attrs {
		if blockAttrs[a] {
			e.blockLevel = true
		}
		if flexAttrs[a] {
			e.flex = true
		}
		if inlineFlexAttrs[a] {
			e.flex, inline = true, true
		}
	}
	// A block button is flex, not inline-flex: [rst-btn~="block"].
	if v, ok := e.attrs["rst-btn"]; ok && hasWord(v, "block") {
		inline = false
	}
	for _, c := range flexInContext {
		if (c.tag != "" && c.tag != p.name) || (c.attr != "" && !has(e.attrs, c.attr)) {
			continue
		}
		for i := len(stack) - 1; i >= 0; i-- {
			if has(stack[i].attrs, c.ancestor) {
				e.flex, inline = true, c.inline
				break
			}
			if !c.deep {
				break
			}
		}
	}
	// .rst-nm small: the meta line under a row's name is a block.
	if p.name == "small" {
		for i := len(stack) - 1; i >= 0; i-- {
			if hasWord(stack[i].attrs["class"], "rst-nm") {
				e.blockLevel = true
				break
			}
		}
	}
	if e.flex && !inline {
		e.blockLevel = true
	}
	if listElements[p.name] {
		e.flex = true
	}
	e.ownLines = e.blockLevel || e.flex || p.name == "button"
	return e
}

func has(attrs map[string]string, name string) bool { _, ok := attrs[name]; return ok }

func hasWord(list, word string) bool {
	for _, w := range strings.Fields(list) {
		if w == word {
			return true
		}
	}
	return false
}

// silent says whether the tag at p renders nothing at all.
func silent(p piece) bool {
	if silentElements[p.name] {
		return true
	}
	return p.name == "input" && strings.EqualFold(attrsOf(p.text)["type"], "hidden")
}

// Format lays rendered HTML out for reading: a line break and two
// spaces of indent per open element, wherever whitespace there cannot
// change what renders. That is between two tags where one is a block;
// between the children of a flex or grid container, its text included;
// and at the edge of a line, where a space is removed when the line is
// laid out, which is what lets the hidden inputs at the head of a form
// stand one to a line. Text, inline runs, attribute values and
// everything inside a raw element or one whose style preserves
// whitespace are left byte for byte, except the
// spaces at the two ends of a flex item's text, which never render. No
// column limit is promised: an inline run stays one line however long
// it is, and the page soft-wraps it instead.
func Format(html string) string {
	return layout(html, func(depth int) string { return "\n" + strings.Repeat("  ", depth) })
}

// layout walks the markup and writes brk(depth) wherever Format may
// break, and the original bytes everywhere else.
func layout(s string, brk func(depth int) string) string {
	ps := pieces(s)
	var b strings.Builder
	var stack []elem
	parent := func() *elem {
		if len(stack) == 0 {
			return nil
		}
		return &stack[len(stack)-1]
	}
	// edge: nothing visible since the start of the current line. The
	// fragment starts at one: every sample sits in a block.
	edge := true
	// gap: the last thing written was a tag, or a flex item's text, and
	// only the whitespace in pending has followed it. prevBlock is
	// whether that tag was a block's.
	gap, prevBlock := false, false
	// opened: the last tag written opened the element on top of the
	// stack. A line break between it and its own end would put
	// whitespace in an element that had none, and :empty, which the
	// shells use to hide an empty foot, would stop matching.
	opened := false
	pending := ""
	for i := 0; i < len(ps); i++ {
		p := ps[i]
		switch p.kind {
		case commentPiece:
			b.WriteString(pending)
			b.WriteString(p.text)
			pending, gap, opened = "", false, false
			continue
		case textPiece:
			core := strings.TrimSpace(p.text)
			if core == "" && gap {
				pending += p.text
				continue
			}
			if par := parent(); par != nil && par.flex && par.elements && core != "" {
				// A flex item of its own: the spaces at its two ends fall
				// at the edges of its line and never render.
				if gap {
					par.broken = true
					b.WriteString(brk(len(stack)))
				} else {
					b.WriteString(p.text[:strings.Index(p.text, core)])
				}
				b.WriteString(core)
				pending = p.text[strings.Index(p.text, core)+len(core):]
				gap, prevBlock, edge, opened = true, false, false, false
				continue
			}
			b.WriteString(pending)
			b.WriteString(p.text)
			pending, gap, opened = "", false, false
			if core != "" {
				edge = false
			}
			continue
		}

		closes := p.end && parent() != nil && parent().name == p.name
		inFlex := parent() != nil && parent().flex
		var e elem
		if !p.end {
			e = describeElem(p, stack)
		}
		thisBlock := blockElements[p.name] || (!p.end && e.blockLevel) || (closes && parent().blockLevel)
		// Never into the start of an inline element: its first child on a
		// line of its own while its end shares a line with its last
		// reads as a mistake, whatever it renders as.
		intoInline := opened && !parent().ownLines
		breakable := gap && !(closes && (opened || !parent().broken)) &&
			(prevBlock || thisBlock || inFlex || !intoInline && (edge || lineEndsAt(ps, i, stack)))
		opened = false
		if closes {
			stack = stack[:len(stack)-1]
		}
		if breakable {
			if par := parent(); par != nil {
				par.broken = true
			}
			b.WriteString(brk(len(stack)))
		} else {
			b.WriteString(pending)
		}
		pending = ""
		b.WriteString(p.text)
		gap, prevBlock = true, thisBlock

		switch {
		case p.end:
			// Past a block the next thing starts a line; past anything
			// else it shares the line with what just closed.
			edge = thisBlock
		case !p.void && preserves(p):
			j := closing(ps, i)
			for k := i + 1; k <= j; k++ {
				b.WriteString(ps[k].text)
			}
			i = j
			if !silent(p) {
				edge = e.blockLevel
			}
			prevBlock = e.blockLevel
		case p.void:
			if p.name == "br" {
				edge = true
			} else if !silent(p) {
				edge = false
			}
		default:
			e.elements = hasElementChild(ps, i)
			stack = append(stack, e)
			opened = true
			if e.ownLines {
				edge = true
			}
		}
	}
	b.WriteString(pending)
	return b.String()
}

// lineEndsAt says whether nothing visible stands between ps[i] and the
// end of the current line: only whitespace and silent elements, then a
// block, a line break, the end of a container that has lines of its own,
// or the end of the fragment. A space there is the last thing on its
// line and is removed when the line is laid out.
func lineEndsAt(ps []piece, i int, stack []elem) bool {
	for k := i; k < len(ps); k++ {
		q := ps[k]
		switch {
		case q.kind == commentPiece:
			continue
		case q.kind == textPiece:
			if strings.TrimSpace(q.text) != "" {
				return false
			}
			continue
		case q.end:
			if len(stack) == 0 || stack[len(stack)-1].name != q.name {
				return blockElements[q.name]
			}
			return stack[len(stack)-1].ownLines
		case silent(q):
			if rawElements[q.name] && !q.void {
				k = closing(ps, k)
			}
			continue
		case q.name == "br" || blockElements[q.name]:
			return true
		default:
			return describeElem(q, stack).blockLevel
		}
	}
	return true
}

// hasElementChild says whether the element opened at ps[i] has a tag
// anywhere before its end.
func hasElementChild(ps []piece, i int) bool {
	end := closing(ps, i)
	for k := i + 1; k < end; k++ {
		if ps[k].kind == tagPiece {
			return true
		}
	}
	return false
}

// closing is the index of the tag that closes the raw element opened
// at ps[i], counting nested elements of the same name; the last piece
// when the markup never closes it.
func closing(ps []piece, i int) int {
	name, open := ps[i].name, 1
	for j := i + 1; j < len(ps); j++ {
		if ps[j].kind != tagPiece || ps[j].name != name {
			continue
		}
		if ps[j].end {
			open--
			if open == 0 {
				return j
			}
		} else if !ps[j].void {
			open++
		}
	}
	return len(ps) - 1
}
