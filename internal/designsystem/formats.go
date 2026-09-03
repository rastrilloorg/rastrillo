package designsystem

import "html/template"

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
	// Markup is the sample, complete HTML with no template actions.
	// Empty means a section with nothing to show, which none has yet.
	Markup string
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
			Body:  "Put a date in a <time> tag, with a version of the date a computer can read. Write the date itself however your readers expect to see it. Rastrillo will not format it for you.",
			Markup: `<section rst-box>
  <dl rst-detail>
    <dt>Published</dt>
    <dd><time datetime="2026-08-02">2 August 2026</time></dd>
  </dl>
</section>`,
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
			Body:  "Break up long numbers so they can be read at a glance: 54,173 in English, 54.173 in German, ٥٤٬١٧٣ in Arabic. Never show a long number as plain digits. In a table, use digits that are all the same width, so the columns line up.",
			Markup: `<div rst-stats>
  <div rst-stat><span rst-stat-label>Members</span><span rst-stat-num>54,173</span></div>
  <div rst-stat><span rst-stat-label>Messages</span><span rst-stat-num>1,284,902</span></div>
</div>`,
		},
		{
			Key:   "money",
			Title: "Money",
			Body:  "Rastrillo only knows how to write US dollars. For anything else, format the amount yourself. Either way, store money as a whole number of cents, never as a decimal.",
			Markup: `<section rst-box>
  <dl rst-detail>
    <dt>Payout</dt>
    <dd>$1,284.50</dd>
  </dl>
</section>`,
		},
		{
			Key:   "ratios",
			Title: "Percentages and bars",
			Body:  "Use <meter> to show how full something is. Use <progress> to show how far a job has got. Always print the number beside the bar. A bar on its own tells a blind reader nothing.",
			Markup: `<div rst-box-head><h2>Photo storage</h2></div>
<section rst-box>
  <span rst-meter><meter rst-meter-bar value="82" min="0" max="100" aria-hidden="true"></meter><span rst-meter-num>412 / 500</span></span>
</section>
<div rst-box-head><h2>Import</h2></div>
<section rst-box>
  <div rst-job><span rst-spin aria-hidden="true"></span> <strong>Import</strong> is running — 128 of 400…<progress rst-job-bar value="32" min="0" max="100" aria-hidden="true"></progress></div>
</section>`,
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
			Markup: `<div rst-card style="--rst-cols: minmax(0, 1fr) 120px">
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
  <p>Every page here meets the Web Content Accessibility Guidelines
  (<abbr title="Web Content Accessibility Guidelines">WCAG</abbr>) 2.2 at level AA.</p>
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
    <p>Total <output for="seats" name="total">$144.00</output></p>
  </form>
</section>`,
		},
	}
}

// buildFormats renders every section for one theme and locale.
func buildFormats(mount, theme, locale string) []formatView {
	docs := formatDocs()
	out := make([]formatView, 0, len(docs))
	for _, doc := range docs {
		id := anchorID("format", doc.Key)
		out = append(out, formatView{
			Title:  proseIn(locale, doc.Title),
			ID:     id,
			Marker: marker("format", doc.Key),
			Body:   proseIn(locale, doc.Body),
			Preview: newPreview(mount, theme, locale, id+"-0",
				previewTitle(locale, doc.Key, "Dates, numbers and names"), doc.Markup, id),
		})
	}
	return out
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
<div class="ds-head"><h2 id="formats">{{P "Dates, numbers and names"}}</h2></div>
<p class="ds-lead">{{P "How to show dates, numbers, money, names and addresses, and which ones people mix up."}}</p>
{{range .Formats}}
<article class="ds-partial" id="{{.ID}}" data-ds-anchor>
{{.Marker}}
<h3>{{.Title}}</h3>
<p class="ds-lead">{{.Body}}</p>
<div class="ds-sample">
{{template "ds-view" .Preview}}
</div>
</article>
{{end}}
{{end}}`
