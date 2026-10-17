package designsystem

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

// ── Sample content in the page's language ────────────────────────────
//
// The samples speak the language of the page they are on. A Japanese
// page with English buttons was showing how English fits a component,
// which is the one language a reader can already picture: German runs
// long, Arabic runs right to left, and the only way to see either inside
// a component is to put it there. The stand-ins stay as written: a
// person is called Grace Hopper in every language, an address is an
// address and a route is a route.
//
// Every translated sample string is a prose key, looked up through
// proseIn like everything else the page says, so the same three gates
// hold it: TestEveryProseKeyIsTranslated wants eleven translations of
// every string a render asked for, TestNoEnglishProseReachesATranslatedPage
// sweeps for the English on every other page, and the stale check
// fails on a translation nothing renders. The translations themselves
// are in sampleprose.go, apart from the page's own voice in prose.go.
//
// There are two kinds of sample and so two walkers. A partial's sample
// is DATA: localData translates the values a person reads and leaves
// the machine's values alone, and the partial and its Template call are
// both rendered from the result, so the preview and the call cannot
// disagree. A hand-written sample is MARKUP (ui.Styleguide, the formats,
// two screens, a raw state): localMarkup translates its text and the
// attributes a person reads or hears, and the preview and the HTML tab
// are both that one string.
//
// What stays as written is decided by rule rather than listed, so a new
// sample is translated by default and the parity gate asks for its
// eleven translations; see keepsAsWritten. The list that exists,
// sampleStandIns, is the names and brands no rule can recognise.

// sampleCodeKeys are the data keys whose values are a machine's: a URL, a
// tone, a field's type, an icon slug, a date the browser reads. Their
// values are never looked up, whatever they look like; "positive" is a
// tone and "plus" an icon however English they read.
//
// Currency is an ISO 4217 code that field-money posts back beside the
// amount. Translated, the form would post a code no currency has, and
// hasWords reads three capitals as a word, so the rule alone would not
// keep it.
//
// Err is here on purpose. It is the error a job's own code raised, quoted
// as the job reported it, and an app shows the database's English the
// way the database wrote it.
var sampleCodeKeys = map[string]bool{
	"Href": true, "Action": true, "SearchAction": true, "PostAction": true,
	"ActionHref": true, "CancelHref": true, "DoneHref": true, "EscalateHref": true,
	"HomeHref": true, "BackHref": true, "ClearHref": true, "PollURL": true, "Return": true,
	"ID": true, "Tone": true, "Status": true, "Type": true, "Autocomplete": true,
	"Rel": true, "Kind": true, "Seed": true, "ActionIcon": true, "Lead": true,
	"LeadInitial": true, "Initial": true, "Email": true, "DateTime": true,
	"Min": true, "Max": true, "Ref": true, "Exact": true, "Err": true,
	"Currency": true,
	// Scheme is "light" or "dark", the cookie's value the server
	// writes back as data-theme; translated it would post a scheme the
	// handler refuses.
	"Scheme": true,
}

// sampleWordKeys are data keys whose value is words even when it is one
// lowercase word: a search query is what a person typed, and "spam"
// looks exactly like a field name.
var sampleWordKeys = map[string]bool{"Query": true}

// sampleStandIns are the names, brands and fixed values that stay as
// written inside a string that is otherwise translated. "Actions for
// Grace Hopper" is translated around the name; "Grace Hopper" alone is
// not looked up at all. TestSampleTranslationsKeepTheirStandIns holds
// every translation of a key that carries one to carrying it too.
var sampleStandIns = []string{
	// People.
	"Grace Hopper", "Ada Lovelace", "Alan Turing", "Fiona Reid", "Otto Neurath",
	"Hedy Lamarr", "Mary Sherman",
	// Brands and products: the gallery's own sample apps, the framework,
	// and the companies whose buttons the sign-in screen draws.
	"Harbour", "rastrillo", "Google", "Apple", "WCAG",
	// Fixed values a translation would only damage: a shortened count,
	// and a postal address that is somewhere.
	"4.1k", "42 Wharf Road, Dublin 2",
}

var (
	// sampleIdent is a machine's word: a field name, an option's value, a
	// slug. A lowercase letter first, then no spaces.
	sampleIdent = regexp.MustCompile(`^\p{Ll}[\p{L}\p{N}_.\-]*$`)
	// sampleCode is a reference with a digit in it — AB3PX, D02, #4471 —
	// or a one- or two-letter initial.
	sampleCode = regexp.MustCompile(`#?\b(?:[A-Z]*[0-9][A-Z0-9]*|[A-Z]{1,2})\b`)
	sampleMail = regexp.MustCompile(`\S+@\S*`)
)

// keepsAsWritten says whether a sample string stays as it is in every
// language: a URL, an address, a machine's identifier, a number with no
// words around it, or nothing but stand-ins.
func keepsAsWritten(s string) bool {
	s = strings.TrimSpace(html.UnescapeString(s))
	if s == "" || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "?") ||
		strings.HasPrefix(s, "#") && !strings.Contains(s, " ") || strings.Contains(s, "://") {
		return true
	}
	if sampleIdent.MatchString(s) {
		return true
	}
	return !hasWords(s)
}

// hasWords says whether anything a reader would call a word is left in s
// once the stand-ins, the email addresses and the codes are taken out.
func hasWords(s string) bool {
	for _, k := range sampleStandIns {
		s = strings.ReplaceAll(s, k, "")
	}
	s = sampleMail.ReplaceAllString(s, "")
	s = sampleCode.ReplaceAllString(s, "")
	return strings.IndexFunc(s, unicode.IsLetter) >= 0
}

// verb marks a sample word that is an action where the same English is
// a noun elsewhere in the samples: Archive in a row's menu against the
// Archive a topbar links to, Export in a bulk menu against the Export a
// file size describes. English spells the two alike and most of the
// eleven do not (Archivar and Archivo), so the action is looked up
// under its own key, the word plus verbSense. English still reads the
// bare word.
type verb string

const verbSense = " (verb)"

// localData is one partial sample's data in one locale: the same shape,
// with every string a person reads translated. A copy, never the
// original, because families() hands the same maps to every locale.
func localData(locale string, v any) any {
	return localValue(locale, "", v)
}

func localValue(locale, key string, v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = localValue(locale, k, e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = localValue(locale, key, e)
		}
		return out
	case verb:
		s := proseIn(locale, string(v)+verbSense)
		if locale == "en" {
			return string(v)
		}
		return s
	case string:
		if sampleCodeKeys[key] {
			return v
		}
		if sampleWordKeys[key] && v != "" {
			return proseIn(locale, v)
		}
		if keepsAsWritten(v) {
			return v
		}
		return proseIn(locale, v)
	}
	// Hidden's [][2]string pairs, numbers, booleans and markup a sample
	// built itself (a brand's mark) are all the machine's.
	return v
}

// ── Markup ───────────────────────────────────────────────────────────

// spokenAttrs are the attributes a person reads or hears. Every other
// attribute is the machine's, including value on anything but a text
// input: an <option>'s value is what the form posts.
var spokenAttrs = map[string]bool{
	"aria-label": true, "data-tip": true, "placeholder": true, "title": true, "label": true,
}

// keptElements hold content that is never translated: code, a drawing,
// a name (the convention throughout is that a person's name, an email
// address or a record's own words go in <bdi>), and the slots of a
// shell that carry the app's brand and its user.
var keptElements = map[string]bool{"svg": true, "code": true, "bdi": true, "style": true, "script": true}

var keptAttrs = []string{"rst-shell-brand", "rst-person-name", "rst-person-email", "rst-person-av"}

// phraseElements are the elements that sit inside a sentence. An
// element whose content is words and these, and nothing else, is
// translated as one sentence, markup included, so "Displaying
// <strong>1–20</strong> of <strong>412</strong>" can become a sentence
// with the two numbers the other way round. Translated word by word it
// could not: "of" has no translation that works in every order.
var phraseElements = map[string]bool{
	"a": true, "abbr": true, "b": true, "bdi": true, "code": true, "data": true,
	"em": true, "i": true, "output": true, "small": true, "strong": true, "time": true,
}

var voidElements = map[string]bool{
	"area": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// mtoken is one piece of a sample's markup: text, or a tag.
type mtoken struct {
	raw   string
	tag   string // the element's name; empty for text
	close bool   // </tag>
	void  bool   // a void element, or a self-closing SVG child
}

// tokenise splits markup into text and tags. It is not a general HTML
// parser and does not need to be: the samples are this repository's
// own, with every attribute quoted and no comments.
func tokenise(src string) []mtoken {
	var out []mtoken
	for len(src) > 0 {
		i := strings.IndexByte(src, '<')
		if i != 0 {
			if i < 0 {
				i = len(src)
			}
			out = append(out, mtoken{raw: src[:i]})
			src = src[i:]
			continue
		}
		j := tagEnd(src)
		raw := src[:j]
		src = src[j:]
		t := mtoken{raw: raw}
		name := strings.TrimPrefix(raw[1:], "/")
		t.close = strings.HasPrefix(raw, "</")
		if k := strings.IndexAny(name, " \t\n/>"); k >= 0 {
			name = name[:k]
		}
		t.tag = strings.ToLower(name)
		t.void = voidElements[t.tag] || strings.HasSuffix(raw, "/>")
		out = append(out, t)
	}
	return out
}

// tagEnd is the length of the tag at the start of src, quoted attribute
// values included: a > inside one does not end the tag.
func tagEnd(src string) int {
	var quote byte
	for i := 1; i < len(src); i++ {
		switch c := src[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i + 1
		}
	}
	return len(src)
}

// closing is the index of the token that closes the element opened at
// toks[i], or len(toks) when nothing does.
func closing(toks []mtoken, i int) int {
	depth := 0
	for j := i + 1; j < len(toks); j++ {
		t := toks[j]
		if t.tag != toks[i].tag || t.void {
			continue
		}
		if !t.close {
			depth++
			continue
		}
		if depth == 0 {
			return j
		}
		depth--
	}
	return len(toks)
}

// localMarkup is one hand-written sample in one locale. In English it
// returns src byte for byte; anywhere else the only bytes that change
// are the text and the spoken attribute values it translates.
func localMarkup(locale, src string) string {
	toks := tokenise(src)
	var b strings.Builder
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.tag == "":
			b.WriteString(localText(locale, t.raw))
		case t.close || t.void:
			b.WriteString(localAttrs(locale, t))
		default:
			end := closing(toks, i)
			b.WriteString(localAttrs(locale, t))
			if keptElement(t) {
				for _, k := range toks[i+1 : min(end+1, len(toks))] {
					b.WriteString(k.raw)
				}
				i = end
				continue
			}
			if isPhrase(toks[i+1 : end]) {
				var inner strings.Builder
				for _, k := range toks[i+1 : end] {
					inner.WriteString(k.raw)
				}
				b.WriteString(localText(locale, inner.String()))
				if end < len(toks) {
					b.WriteString(toks[end].raw)
				}
				i = end
			}
		}
	}
	return b.String()
}

// keptElement says whether an element's content stays as written.
func keptElement(t mtoken) bool {
	if keptElements[t.tag] {
		return true
	}
	for _, a := range keptAttrs {
		if strings.Contains(t.raw, " "+a+" ") || strings.Contains(t.raw, " "+a+">") || strings.Contains(t.raw, " "+a+"=") {
			return true
		}
	}
	return false
}

// isPhrase says whether an element's content is one sentence: words of
// its own beside at least one element, and every element inside it one
// that sits in running text.
func isPhrase(inner []mtoken) bool {
	words, elements := false, false
	depth := 0
	for _, t := range inner {
		switch {
		case t.tag == "":
			if depth == 0 && hasWords(html.UnescapeString(t.raw)) {
				words = true
			}
		case !phraseElements[t.tag]:
			return false
		case t.close:
			depth--
		default:
			if depth == 0 {
				elements = true
			}
			if !t.void {
				depth++
			}
		}
	}
	return words && elements
}

// localText translates one run, keeping the whitespace around it where
// it was: the samples are indented, and the indent is not the sentence.
func localText(locale, raw string) string {
	core := strings.TrimSpace(raw)
	if core == "" || keepsAsWritten(stripTags(core)) {
		return raw
	}
	lead := raw[:strings.Index(raw, core)]
	return lead + proseIn(locale, core) + raw[len(lead)+len(core):]
}

// stripTags is a phrase's text with its markup taken out, for deciding
// whether there is anything in it to translate.
func stripTags(s string) string {
	var b strings.Builder
	for _, t := range tokenise(s) {
		if t.tag == "" {
			b.WriteString(t.raw)
		}
	}
	return b.String()
}

// spokenAttr matches one double-quoted attribute and its value.
var spokenAttr = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)

// localAttrs translates the spoken attributes of one tag. A text input's
// value is spoken too: it is what the field shows.
func localAttrs(locale string, t mtoken) string {
	if t.close || !strings.Contains(t.raw, `="`) {
		return t.raw
	}
	textInput := t.tag == "input" && (strings.Contains(t.raw, ` type="text"`) || !strings.Contains(t.raw, ` type="`))
	return spokenAttr.ReplaceAllStringFunc(t.raw, func(m string) string {
		sub := spokenAttr.FindStringSubmatch(m)
		if !(spokenAttrs[sub[1]] || textInput && sub[1] == "value") || keepsAsWritten(sub[2]) {
			return m
		}
		return sub[1] + `="` + proseIn(locale, sub[2]) + `"`
	})
}
