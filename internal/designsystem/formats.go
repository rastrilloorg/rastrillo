package designsystem

import (
	"fmt"
	"html/template"
	"maps"
	"strings"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/codeview"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/ui"
)

// The Dates, numbers and names page: which tag carries which kind of
// value, and the pairs people confuse (spec §6-v2.4).
//
// It exists because four of these elements have no partial to live in
// and never will. <address>, <abbr>, <data> and <output> are things an
// app writes into its own markup — there is no data shape for the
// framework to wrap — so a component page cannot document them and the
// knowledge had nowhere to go.
//
// The two sections that earn the page on their own are addresses and
// short forms, and both are written as corrections rather than
// descriptions. <address> means authorship and its name says postal
// address, so most uses in the wild are wrong; <abbr> has a reputation
// as the accessible answer to jargon and mostly is not one, because it
// depends on hovering. A page that described those neutrally would
// leave both mistakes intact.

// formatDoc is one section: a heading, a paragraph, and a sample.
type formatDoc struct {
	// Key is the anchor id's suffix and previewHeights' key.
	Key string
	// Title and Body are English, and therefore prose keys.
	Title string
	Body  string
	// Markup is the sample. Where a ui helper writes one of its values,
	// Markup is a template with the helper's call in place of the
	// value, and the Template tab shows it: the call is the advice, and
	// the value it writes differs from one page's locale to the next.
	Markup string
	// Data is what a templated Markup reads, and nil for a sample that
	// is plain HTML.
	Data map[string]any
}

// formatView is one section as the page renders it.
type formatView struct {
	Title   string
	ID      string
	Marker  template.HTML
	Body    string
	Preview previewView
}

// formatDocs is the page, in page order.
//
// Every sample is a fact from a plausible screen rather than a
// demonstration of a tag, because the tags are the point and a sample
// reading "example value" would put the reader's attention on the wrong
// half. The two address samples sit in one frame on purpose: the rule
// is a distinction, and a distinction needs both sides visible at once.
func formatDocs() []formatDoc {
	return []formatDoc{
		{
			Key:   "dates",
			Title: "Dates and times",
			Body:  "Put a date in a `<time>` tag, with a version of the date a computer can read. Use `date` to show it in the format used by the reader's language.",
			Markup: `<section rst-box>
  <dl rst-detail>
    <dt>Published</dt>
    <dd><time datetime="{{.Published.Format "2006-01-02"}}">{{date $L .Published "dateYear"}}</time></dd>
  </dl>
</section>`,
			Data: map[string]any{
				"Published": time.Date(2026, time.August, 2, 0, 0, 0, 0,
					time.UTC),
			},
		},
		{
			Key:   "durations",
			Title: "How long something took",
			Body:  "Use <time> for a length of time too. The computer-readable version looks strange: PT2H30M means 2 hours 30 minutes.",
			Markup: `<section rst-box>
  <dl rst-detail>
    <dt>Build time</dt>
    <dd><time datetime="PT2H30M">2h 30m</time></dd>
  </dl>
</section>`,
		},
		{
			Key:   "numbers",
			Title: "Numbers",
			Body:  "Break up long numbers so they can be read at a glance: 54,173 in English, 54.173 in German, ৫৪,১৭৩ in Bengali. Show them with `number`, which groups the digits according to the reader's language. In a table, use digits that are all the same width, so the columns line up.",
			Markup: `<div rst-stats>
  <div rst-stat><span rst-stat-label>Members</span><span rst-stat-num>{{number $L .Members}}</span></div>
  <div rst-stat><span rst-stat-label>Messages</span><span rst-stat-num>{{number $L .Messages}}</span></div>
</div>`,
			Data: map[string]any{"Members": 54173, "Messages": 1284902},
		},
		{
			Key:   "money",
			Title: "Money",
			Body:  "Store each amount as a whole number of the currency's smallest unit, such as cents, with its currency code beside it. Show it with `money`, which writes it the way the reader's language does. The currency always comes from your data, never from the reader.",
			Markup: `<section rst-box>
  <dl rst-detail>
{{- range .Amounts}}
    <dt>{{.Currency}}</dt>
    <dd>{{money $L .Minor .Currency}}</dd>
{{- end}}
  </dl>
</section>`,
			Data: map[string]any{"Amounts": moneyAmounts},
		},
		{
			Key:   "ratios",
			Title: "Percentages and bars",
			Body:  "Use <meter> to show how full something is. Use <progress> to show how far a job has got. Always print the number beside the bar. A bar on its own tells a blind reader nothing.",
			Markup: `<div rst-box-head><h2>Photo storage</h2></div>
<section rst-box>
  <span rst-meter><meter rst-meter-bar value="82" min="0" max="100" aria-hidden="true"></meter><span rst-meter-num>{{number $L .Used}} / {{number $L .Limit}}</span></span>
</section>
<div rst-box-head><h2>Import</h2></div>
<section rst-box>
  <div rst-job><span rst-spin aria-hidden="true"></span> <strong>Import</strong> is running: {{number $L .Done}} of {{number $L .Total}}…<progress rst-job-bar value="32" min="0" max="100" aria-hidden="true"></progress></div>
</section>`,
			Data: map[string]any{
				"Used": 412, "Limit": 500, "Done": 128, "Total": 400,
			},
		},
		{
			Key:   "filesizes",
			Title: "File sizes",
			Body:  "Show a rounded size like 4 MB, and put the exact number of bytes in a <data> tag. Screen readers read what is on screen and ignore the tag, so the rounded size has to be useful on its own.",
			Markup: `<section rst-box>
  <dl rst-detail>
    <dt>Export</dt>
    <dd><data value="4194304">4 MB</data></dd>
  </dl>
</section>`,
		},
		{
			Key:   "identifiers",
			Title: "Reference numbers and codes",
			Body:  "Do not break these up. Order 4471, not order 4,471. Years, version numbers and port numbers are the same. Use rst-mono so they look like codes, and never put one in a <time> tag, even when it looks like a date.",
			Markup: `<section rst-box>
  <dl rst-detail>
    <dt>Reference</dt>
    <dd class="rst-mono">post_01H9ZQ</dd>
    <dt>Order</dt>
    <dd class="rst-mono">4471</dd>
  </dl>
</section>`,
		},
		{
			Key:   "people",
			Title: "People's names",
			Body:  "A name can be in any language, including ones that read right to left. When a name sits in the middle of a line, wrap it in <bdi>, or it will scramble the words around it. Use person to show a name with a picture beside it.",
			Markup: `<div rst-card class="people">
  <div rst-lrow>
    <a class="rst-nm" href="/people/1"><bdi>Grace Hopper</bdi><small><bdi>grace@example.com</bdi> · 09:12</small></a>
    <span class="rst-cell-mut rst-m-hide">Admin</span>
  </div>
</div>`,
		},
		{
			Key:   "addresses",
			Title: "Addresses",
			Body:  "The <address> tag is not for postal addresses. It is for saying who wrote the page. A delivery address is ordinary text. A list of staff is a list.",
			Markup: `<section rst-box>
  <address>Written by <bdi>Grace Hopper</bdi> · <a href="mailto:grace@example.com">grace@example.com</a></address>
</section>
<section rst-box>
  <dl rst-detail>
    <dt>Deliver to</dt>
    <dd>42 Wharf Road, Dublin 2</dd>
  </dl>
</section>`,
		},
		{
			Key:   "abbr",
			Title: "Short forms like WCAG",
			Body:  "The <abbr> tag shows the full wording when you hover. That does not work on a phone, and screen readers often skip it. Write the words out in full the first time as well.",
			Markup: `<section rst-box>
  <p>Every page here meets the Web Content Accessibility Guidelines (<abbr title="Web Content Accessibility Guidelines">WCAG</abbr>) 2.2 at level AA.</p>
</section>`,
		},
		{
			Key:   "output",
			Title: "Numbers your page works out",
			Body:  "When your page adds something up, put the answer in an <output> tag. A running total, a converted price, a countdown.",
			Markup: `<section rst-box>
  <form rst-form>
    <div rst-field>
      <label rst-field-label for="seats">Seats</label>
      <input rst-input="short" id="seats" name="seats" type="number" value="12">
    </div>
    <p>Total <output for="seats" name="total">{{money $L .Total .Currency}}</output></p>
  </form>
</section>`,
			Data: map[string]any{"Total": 14400, "Currency": "USD"},
		},
	}
}

// formatSource is one section's sample in a locale as its source: the
// markup, or for a templated sample the template the Template tab
// shows, with its words translated by localMarkup. Translating before
// the template runs keeps what the helpers write out of the parity
// gate: "August" and "KWD" read as words to hasWords, and a value a
// helper wrote for a locale is not English to translate.
func formatSource(doc formatDoc, locale string) string {
	return localMarkup(locale, doc.Markup)
}

// formatMarkup is one section's sample as a reader sees it in a
// locale: formatSource, run for a templated sample, so each helper
// writes its value the way an app's page in that locale would.
// Shared by buildFormats and TestTheTextOnScreenIsTheTextCopied, so the
// two cannot read a sample differently.
func formatMarkup(doc formatDoc, locale string) string {
	source := formatSource(doc, locale)
	if doc.Data == nil {
		return source
	}
	tmpl, err := template.New(doc.Key).Funcs(ui.Funcs()).
		Parse(`{{$L := localizer $}}` + source)
	if err != nil {
		// The samples are fixed and their translations carry each call
		// as written (TestSampleTranslationsKeepTheirStandIns), so a
		// failure here is a bug in this file, not a request to handle.
		panic(fmt.Sprintf("designsystem: the %s sample: %v", doc.Key, err))
	}
	data := maps.Clone(doc.Data)
	data["L"] = ui.NewLocalizer(locale, formatLocale(locale),
		translator(locale))
	var written strings.Builder
	if err := tmpl.Execute(&written, data); err != nil {
		panic(fmt.Sprintf("designsystem: the %s sample: %v", doc.Key, err))
	}
	return written.String()
}

// buildFormats renders every section for one theme and locale.
func buildFormats(mount, theme, locale string, files previewSet) ([]formatView, error) {
	docs := formatDocs()
	out := make([]formatView, 0, len(docs))
	for _, doc := range docs {
		id := anchorID("format", doc.Key)
		markup := formatMarkup(doc, locale)
		preview, file := newPreview(mount, theme, locale, "formats", id+"-0",
			previewTitle(locale, doc.Key, "Dates, numbers and names"), markup, markup, id)
		if err := files.add(file); err != nil {
			return nil, err
		}
		if doc.Data != nil {
			preview.Call = codeview.Highlight(formatSource(doc, locale))
		}
		out = append(out, formatView{
			Title:   proseIn(locale, doc.Title),
			ID:      id,
			Marker:  marker("format", doc.Key),
			Body:    proseIn(locale, doc.Body),
			Preview: preview,
		})
	}
	return out, nil
}

// moneyAmounts is the sample's three stored amounts: minor units and
// an ISO 4217 code, the only two fields a stored amount carries
// (decision 1). Three currencies rather than one, because the body's
// rule — the currency always comes from the data, never from the
// reader — has nothing to show against a single currency: the euro
// must stay a euro on the Japanese page for the rule to be visible at
// all.
var moneyAmounts = []storedAmount{
	{128450, "EUR"},
	{1285, "JPY"},
	{1284500, "KWD"},
}

// storedAmount is one amount as an app stores it.
type storedAmount struct {
	Minor    int64
	Currency string
}

// formatLocales is the gallery's offered locale codes mapped to the
// CLDR locale the helpers write values in, per CheckConfig
// (locales/config.go). It is computed once rather than per page: the
// mapping is fixed for the life of the binary, and CheckConfig walks
// every shipped pack to build it.
var formatLocales = sync.OnceValue(func() map[string]string {
	cfg, err := locales.CheckConfig("en", rastrillo.BaseLocales())
	if err != nil {
		// Every base locale's pack is blank-imported in packs.go
		// specifically so this never happens; a failure here means
		// that file has drifted from BaseLocales, not that a reader
		// hit a bad request.
		panic(fmt.Sprintf("designsystem: gallery locale configuration: %v", err))
	}
	return cfg.Formats
})

// formatLocale is the CLDR locale a gallery locale's values are
// written in.
func formatLocale(locale string) string {
	if format, ok := formatLocales()[locale]; ok {
		return format
	}
	return locale
}

// formatNav is this page's rail entries.
func formatNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("formats")
	items := make([]navItem, 0, len(view.Formats))
	for _, f := range view.Formats {
		items = append(items, navItem{Label: f.Title, Href: anchorHrefIn(mount, theme, locale, file, f.ID)})
	}
	return items
}

const formatsBody = `{{define "ds-body-formats"}}
<p class="ds-lead">{{P "How to show dates, numbers, money, names and addresses, and which ones people mix up."}}</p>
<p class="ds-lead">{{P "Each example is live, but its links go nowhere."}}</p>
{{range .Formats}}
<article class="ds-partial" id="{{.ID}}" data-ds-anchor>
{{.Marker}}
<h2 class="ds-partial__name">{{.Title}}</h2>
<p class="ds-lead">{{.Body}}</p>
<div class="ds-sample">
{{template "ds-view" .Preview}}
</div>
</article>
{{end}}
{{end}}`
