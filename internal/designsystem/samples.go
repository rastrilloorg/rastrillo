package designsystem

import "github.com/carlosframework/rastrillo"

// This file is the page's content: which partial goes in which family,
// which states of it are worth showing, and the one data value each of
// those states is built from. Nothing here decides layout — page.go
// does that — and nothing here is generated: a partial's interesting
// states are a judgement, and the judgement is the documentation.
//
// Two rules the data has to keep, both of them silent when broken:
//
//   - Every input Name on the page is unique. Every field partial
//     derives its id, its hint id and its error id from Name, so two
//     samples sharing one Name duplicate three ids and point one
//     field's aria-describedby at another field's message.
//   - Sample text is real. "Lorem ipsum" in a component library is how
//     a component that cannot hold a real sentence ships looking fine.
//
// The partial strings a sample does NOT set are the point of several of
// them: the framework's own defaults resolve through T, so an unset
// Label renders in the page's language rather than in English.
//
// Every English string in this file — a family's Title and Blurb, a
// partial's Blurb, a state's State and Note — is the page speaking in
// its own voice, and the page speaks twelve languages. The renderer
// passes each of them through proseIn, so the string you write here is
// BOTH the English and the lookup key: add a state with a note and
// TestEveryProseKeyIsTranslated fails until prose.go carries the eleven
// translations. That is why the English stays here, in among the data
// it labels, rather than moving out to a table of slugs — see prose.go
// for the whole of that argument. Sample DATA is exempt and stays
// English on every page: a person is called Grace Hopper in Japanese
// too, and a route is /posts/new in Arabic.

// wrapper is the container a sample needs around it, per the rules in
// docs/site/templates.md: rst-list and rst-card hold rows only, and a
// form goes in the padded rst-box. A sample in the wrong container is
// exactly the mistake the page is meant to stop.
type wrapper int

const (
	wrapBare wrapper = iota // nothing around it
	wrapList                // <div class="rst-list">…</div>
	wrapForm                // <section class="rst-box"><form class="rst-form">…</form></section>
	wrapBox                 // <section class="rst-box">…</section>
)

// sample is one state of one partial: an English label for the state,
// the data value the partial takes, and optionally a note explaining
// what to look at.
//
// Raw is the escape hatch for markup no partial emits — a hand-written
// <select> with <optgroup>, say. It is parsed as a template with the
// page's own funcs, so a raw sample still localises through T.
type sample struct {
	State string
	Data  any
	Raw   string
	Note  string

	// Build supplies Data that only exists once the page's locale is
	// known — locale-menu's twelve items, and nothing else so far.
	Build func(locale string) any
}

// partialDoc is one partial's whole section on the page.
type partialDoc struct {
	Name   string
	Blurb  string
	Wrap   wrapper
	States []sample
}

// family groups partials the way ui's own doc comment does: the list
// screen, the display vocabulary, the form controls, the date and time
// fields, and the route-level pages.
//
// A family is a PAGE of the gallery now, not a section of one — see
// pageKinds() in page.go, which reads its component rows straight off
// this table. Two consequences worth knowing before editing here:
//
//   - Title and Blurb are the page's own name and the sentence the
//     Overview routes readers to it with, so they are prose keys twice
//     over and a new family needs its eleven translations before
//     anything builds;
//   - every partial ui.Templates() defines must appear in some family.
//     A partial no family claims used to land in an "Ungrouped" section
//     of the one components page; there is no such page to land on any
//     more, so buildFamilies fails instead and names the partial.
type family struct {
	Key      string
	Title    string
	Blurb    string
	Partials []partialDoc
}

// tones is the tone vocabulary every tinted component shares. Rendering
// all of them side by side is the only way a missing pair in a new theme
// shows up as something other than a passing contrast test.
var tones = []string{"neutral", "positive", "warning", "negative"}

// selectOptions builds an n-option list for field-select. Past ten
// options the partial arms select.js, so the same builder produces both
// the plain native select and the enhanced combobox.
func selectOptions(labels ...string) []any {
	out := make([]any, 0, len(labels))
	for i, label := range labels {
		opt := map[string]any{"Value": label, "Label": label}
		if i == 0 {
			opt["Selected"] = true
		}
		out = append(out, opt)
	}
	return out
}

// families is the whole page, in page order.
func families() []family {
	return []family{
		{
			Key:   "list-screen",
			Title: "List screen",
			Blurb: "Generic list screen with title, toolbar, rows that work desktop and mobile.",
			Partials: []partialDoc{
				{
					Name:  "page-header",
					Blurb: "The screen's title bar: an h1, an optional subhead, and a primary action.",
					States: []sample{
						{State: "Title, subhead and a primary action", Data: map[string]any{
							"Title": "Posts", "Sub": "Everything you have written, newest first.",
							"ActionHref": "/posts/new", "ActionLabel": "Write a post", "ActionIcon": "plus",
						}},
						{State: "Title only", Data: map[string]any{"Title": "Settings"}},
					},
				},
				{
					Name:  "list-bar",
					Blurb: "The toolbar at the top of a list card with search and filter.",
					Wrap:  wrapList,
					States: []sample{
						{State: "Search with a filter dropdown", Data: map[string]any{
							"SearchAction": "/posts", "Query": "release", "Placeholder": "Search posts",
							"Hidden": [][2]string{{"sort", "newest"}},
							"Filter": map[string]any{
								"Label": "Paid", "Aria": "Filter by status: Paid",
								"Items": []any{
									map[string]any{"Href": "/posts", "Label": "All"},
									map[string]any{"Href": "/posts?status=paid", "Label": "Paid", "Current": true},
									map[string]any{"Href": "/posts?status=free", "Label": "Free"},
								},
							},
						}},
					},
				},
				{
					Name:  "list-bar-search",
					Blurb: "Solo search form for search-only lists. An ordinary method=\"get\" form.",
					Wrap:  wrapList,
					States: []sample{
						{State: "With a query", Data: map[string]any{
							"Action": "/comments", "Query": "spam", "Placeholder": "Search comments",
							"Hidden": [][2]string{{"status", "held"}},
						},
							Note: "The ✕ is a link to the same screen with the query dropped. The status filter rides in Hidden and survives it."},
						{State: "With a query on page 4", Data: map[string]any{
							"Action": "/comments", "Query": "spam", "Placeholder": "Search comments",
							"Hidden": [][2]string{{"page", "4"}}, "ClearHref": "/comments",
						},
							Note: "Nothing in Hidden says which pair is the page, so the default would carry page=4 into a result set that no longer has four pages. ClearHref is how an app resets it."},
						{State: "Empty, accessible name from the catalog", Data: map[string]any{"Action": "/comments"},
							Note: "No Placeholder. The input's accessible name comes from the framework catalog in the current language."},
					},
				},
				{
					Name:  "list-search-submit",
					Blurb: "The search form's real submit button: visually hidden, in the tab order, and it un-hides on keyboard focus.",
					Wrap:  wrapBox,
					States: []sample{
						{State: "Default label", Data: map[string]any{},
							Note: "Nothing is visible here until you tab into it."},
					},
				},
				{
					Name:  "list-row-action",
					Blurb: "One list row: the record's name as the primary link, a meta line, and a secondary action.",
					Wrap:  wrapList,
					States: []sample{
						{State: "Lead marker, status and an action", Data: map[string]any{
							"Href": "/posts/1", "Main": "Release notes, August",
							"Sub":        "Published 2 August · 4 min read",
							"ActionHref": "/posts/1/edit", "ActionLabel": "Edit",
							"ActionAria": "Edit Release notes, August",
							"StatusTone": "positive", "StatusLabel": "Published",
							"Lead": "accent", "LeadInitial": "RN",
						}},
						{State: "Name and meta line only", Data: map[string]any{
							"Href": "/posts/2", "Main": "Why we moved off the old runner",
							"Sub": "Draft · saved 20 minutes ago",
						}},
					},
				},
				{
					Name:  "seg-tabs",
					Blurb: "A segmented control of server-rendered links. The current tab is aria-current on a link.",
					States: []sample{
						{State: "Two sections", Data: map[string]any{
							"Label": "Post sections",
							"Items": []any{
								map[string]any{"Label": "Basics", "Href": "?tab=basics", "Current": true},
								map[string]any{"Label": "Advanced", "Href": "?tab=advanced"},
							},
						}},
					},
				},
				{
					Name:  "dropdown",
					Blurb: "A zero-JavaScript disclosure: a summary button over a panel of plain links.",
					States: []sample{
						{State: "A filter menu with one choice applied", Data: map[string]any{
							"Label": "Newest", "Aria": "Sort posts: newest first",
							"Items": []any{
								map[string]any{"Href": "/posts?sort=newest", "Label": "Newest", "Current": true},
								map[string]any{"Href": "/posts?sort=oldest", "Label": "Oldest"},
								map[string]any{"Href": "/posts?sort=title", "Label": "By title"},
							},
						}},
					},
				},
				{
					Name:  "bulk-bar",
					Blurb: "Select mode's header strip, replacing the list bar: the count, an escalate link, and the actions menu.",
					Wrap:  wrapList,
					States: []sample{
						{State: "Three rows selected", Data: map[string]any{
							"DoneHref": "/posts", "Count": "3 selected",
							"EscalateHref": "/posts?select=all", "EscalateLabel": "Select all 412 matching",
							"MenuLabel": "Actions",
							"Actions": []any{
								map[string]any{"Value": "export", "Label": "Export"},
								map[string]any{"Value": "unpublish", "Label": "Unpublish"},
								map[string]any{"Value": "delete", "Label": "Delete…", "Danger": true},
							},
						}, Note: "The destructive item is last: the first entry is the surrounding form's implicit-Enter default."},
					},
				},
				{
					Name:  "pagination",
					Blurb: "A numbered strip of ordinary links, so paging survives JavaScript being off.",
					States: []sample{
						{State: "Middle of a long list", Data: map[string]any{
							"Label": "Posts pages",
							"Items": []any{
								map[string]any{"Label": "Previous", "Href": "/posts?page=3"},
								map[string]any{"Label": "1", "Href": "/posts?page=1"},
								map[string]any{"Gap": true},
								map[string]any{"Label": "3", "Href": "/posts?page=3"},
								map[string]any{"Label": "4", "Current": true},
								map[string]any{"Label": "5", "Href": "/posts?page=5"},
								map[string]any{"Gap": true},
								map[string]any{"Label": "9", "Href": "/posts?page=9"},
								map[string]any{"Label": "Next", "Href": "/posts?page=5"},
							},
						}},
						{State: "First page, default accessible name", Data: map[string]any{
							"Items": []any{
								map[string]any{"Label": "Previous", "Disabled": true},
								map[string]any{"Label": "1", "Current": true},
								map[string]any{"Label": "2", "Href": "/posts?page=2"},
								map[string]any{"Label": "Next", "Href": "/posts?page=2"},
							},
						}, Note: "No Label, so the nav's accessible name comes from the catalog."},
					},
				},
				{
					Name:  "empty-state",
					Blurb: "The blank-state card. Empty state with affordances for starting steps.",
					States: []sample{
						{State: "With a POST call to action", Data: map[string]any{
							"Title":      "Nothing here yet",
							"Body":       "No posts yet. Your first one is a good place to start.",
							"PostAction": "/posts/seed", "ActionLabel": "Add sample posts",
							"Hidden": [][2]string{{"csrf", "tok-123"}},
						}},
						{State: "Sentence only", Data: map[string]any{
							"Body": "No comments on this post yet.",
						}},
					},
				},
			},
		},
		{
			Key:   "display",
			Title: "Display",
			Blurb: "The small pieces of vocabulary a screen is assembled from: status, identity, progress and the one alert shape.",
			Partials: []partialDoc{
				{
					Name:   "status-pill",
					Blurb:  "A record's status as a tinted pill, colour and label.",
					States: statusPillStates(),
				},
				{
					Name:   "badge",
					Blurb:  "An uppercase bordered chip for small static markers. Distinct from status-pill, which is record status.",
					States: badgeStates(),
				},
				{
					Name:   "meter",
					Blurb:  "A capacity bar with its number.",
					States: meterStates(),
				},
				{
					Name:  "person",
					Blurb: "Initials avatar, name and email. Quickly identifies people records.",
					States: []sample{
						{State: "Name and email", Data: map[string]any{
							"Href": "/people/1", "Name": "Grace Hopper", "Email": "grace@example.com", "Initial": "G",
						}},
						{State: "Large, the show-header size", Data: map[string]any{
							"Href": "/people/2", "Name": "Ada Lovelace", "Email": "ada@example.com", "Initial": "A", "Large": true,
						}},
						{State: "No initial", Data: map[string]any{
							"Href": "/people/3", "Name": "Unclaimed invitation", "Email": "invited@example.com",
						}},
					},
				},
				{
					Name:   "callout",
					Blurb:  "The one alert vocabulary: tinted strip, tone icon, optional bold title, one body paragraph.",
					States: calloutStates(),
				},
				{
					Name:  "detail-list",
					Blurb: "Definition list about a record on a detail screen. Mono marks for machine-ish values.",
					Wrap:  wrapBox,
					States: []sample{
						{State: "Four facts, one machine-ish", Data: map[string]any{
							"Items": []any{
								map[string]any{"Label": "Audience", "Value": "Members"},
								map[string]any{"Label": "Main page", "Value": "No"},
								map[string]any{"Label": "Published", "Value": "2 August 2026"},
								map[string]any{"Label": "Reference", "Value": "post_01H9ZQ", "Mono": true},
							},
						}},
					},
				},
				{
					Name:  "notice",
					Blurb: "A one-line confirmation after a redirect. Takes a plain string, not a dict, and renders nothing for an empty one.",
					States: []sample{
						{State: "A saved message", Data: "Post saved."},
					},
				},
				{
					Name:  "form-error",
					Blurb: "A form's summary error line, role=\"alert\". Takes a plain string; renders nothing when there is no error.",
					States: []sample{
						{State: "A rejected submission", Data: "That title is already taken."},
					},
				},
				{
					Name:  "job-status",
					Blurb: "A background job's polled fragment. The data-poll attribute is emitted only while the job runs — omitting it is how polling stops.",
					States: []sample{
						{State: "Running", Data: map[string]any{
							"Name": "Import", "Status": "running", "Progress": "128 of 400",
							"PollURL": "/jobs/1/fragment", "PollSeconds": 2,
						}, Note: "This one polls: it is the only sample on the page that asks the network for anything, and on a static site it simply never resolves."},
						{State: "Finished", Data: map[string]any{"Name": "Import", "Status": "done"}},
						{State: "Failed", Data: map[string]any{
							"Name": "Import", "Status": "failed", "Err": "row 91: no such column \"published\"",
						}},
					},
				},
			},
		},
		{
			Key:   "form",
			Title: "Form",
			Blurb: "Every control, in the states a real form puts it in. Each one sits in a padded rst-box, because that is the rule these partials assume.",
			Partials: []partialDoc{
				{
					Name:   "field",
					Blurb:  "Label, input, optional hint, help and error. The general text-like control, with an explicit ID.",
					Wrap:   wrapForm,
					States: fieldStates(),
				},
				{
					Name:   "field-text",
					Blurb:  "One labelled text input, ids derived from Name. The required marker's star is aria-hidden. The input's required attribute is the programmatic signal.",
					Wrap:   wrapForm,
					States: fieldTextStates(),
				},
				{
					Name:  "field-textarea",
					Blurb: "field-text's wrapper around a textarea.",
					Wrap:  wrapForm,
					States: []sample{
						{State: "Bare", Data: map[string]any{"Name": "ta_bare", "Label": "Notes", "Rows": 3}},
						{State: "With a hint", Data: map[string]any{
							"Name": "ta_hint", "Label": "Bio", "Rows": 3, "Hint": "Shown on your profile.",
						}},
						{State: "With an error", Data: map[string]any{
							"Name": "ta_error", "Label": "Summary", "Rows": 3,
							"Value": "…", "Error": "A summary needs at least twenty characters.",
						}},
						{State: "Required", Data: map[string]any{
							"Name": "ta_required", "Label": "Reason for refund", "Rows": 3, "Required": true,
						}},
					},
				},
				{
					Name:   "field-select",
					Blurb:  "field's envelope around a native select. Past ten options the select arms the filterable combobox in select.js; the native select is still what the form submits.",
					Wrap:   wrapForm,
					States: fieldSelectStates(),
				},
				{
					Name:  "field-check",
					Blurb: "The one toggle mechanism: a real checkbox skinned as a switch, keyboard and screen-reader intact.",
					Wrap:  wrapForm,
					States: []sample{
						{State: "On", Data: map[string]any{"Name": "notify_on", "Label": "Email me about replies", "Checked": true}},
						{State: "Off", Data: map[string]any{"Name": "notify_off", "Label": "Email me about mentions"}},
						{State: "Disabled", Data: map[string]any{
							"Name": "notify_disabled", "Label": "Email me a weekly digest", "Disabled": true,
						}},
					},
				},
				{
					Name:  "choice-field",
					Blurb: "Option cards — the preferred radio or checkbox group. Title stays short; Desc explains.",
					Wrap:  wrapForm,
					States: []sample{
						{State: "Pick one", Data: map[string]any{
							"Legend": "Plan", "Name": "plan",
							"Options": []any{
								map[string]any{"Value": "free", "Title": "Free", "Desc": "One project, community support."},
								map[string]any{"Value": "pro", "Title": "Pro", "Desc": "Unlimited projects and a support inbox.", "Checked": true},
							},
						}},
						{State: "Pick any", Data: map[string]any{
							"Legend": "Send me", "Name": "digest", "Type": "checkbox",
							"Options": []any{
								map[string]any{"Value": "replies", "Title": "Replies", "Desc": "As they happen.", "Checked": true},
								map[string]any{"Value": "weekly", "Title": "Weekly digest", "Desc": "Monday mornings."},
							},
						}},
					},
				},
				{
					Name:  "form-foot",
					Blurb: "A form's closing row: one primary submit and an optional cancel link. Cancel is an anchor, because leaving a form is navigation.",
					Wrap:  wrapForm,
					States: []sample{
						{State: "Save and cancel", Data: map[string]any{
							"Submit": "Save post", "CancelHref": "/posts", "CancelLabel": "Back to posts",
						}},
						{State: "Submit only", Data: map[string]any{"Submit": "Send invitation"}},
						// The busy rule, shown as the pair it is. The
						// second state is Raw rather than data, because
						// it is not a state the partial can be asked
						// for: it is what rastrillo.js writes over the
						// first one the moment the form goes out.
						{State: "Idle — the button before anything happens", Data: map[string]any{
							"Submit": "Publish", "CancelHref": "/posts", "CancelLabel": "Back to posts",
						}, Note: "A button that CHANGES something gets a loading state; a button that only reveals something, e.g. a disclosure, a dropdown, a tab, does not. rastrillo.js applies that rule to every submit button in every form, with nothing to opt into."},
						{State: "Working — what rastrillo.js writes on the way out",
							Note: "Only the button that was clicked: every other submit button in the form keeps its name and its value, and the form itself is guarded against a second submit. data-busy=\"false\" opts out, on the form or on one button; data-busy-label replaces the text. With scripts off none of this happens and the form submits exactly as it always did, so idempotency stays the server's job.",
							Raw: `<div class="rst-form__foot">
<button class="rst-btn rst-btn--primary" type="submit" aria-busy="true" data-idle-label="Publish" disabled><span class="rst-spin rst-btn__spin" aria-hidden="true"></span>Publishing…</button>
<a class="rst-btn" href="/posts">Back to posts</a>
</div>`,
						},
					},
				},
				{
					Name:  "confirm-form",
					Blurb: "The confirm page's action row. Destructive actions get their own route rather than a dialog, so there is nothing to script.",
					Wrap:  wrapBox,
					States: []sample{
						{State: "Destructive", Data: map[string]any{
							"Action": "/posts/1/delete", "Label": "Delete this post", "Danger": true,
							"Hidden":     [][2]string{{"csrf", "tok-123"}, {"id", "1"}},
							"CancelHref": "/posts/1",
						}},
						{State: "Ordinary, default cancel wording", Data: map[string]any{
							"Action": "/posts/1/publish", "Label": "Publish", "CancelHref": "/posts/1",
						}, Note: "No CancelLabel, so the cancel link is worded from the catalog."},
					},
				},
			},
		},
		{
			Key:   "date-and-time",
			Title: "Date and time",
			Blurb: "The four fields datetime.js enhances, each keeping its native input underneath, so a reader with no script gets the plain control and not a broken one.",
			Partials: []partialDoc{
				{
					Name:   "field-date",
					Blurb:  "A native date input, plus the natural-language combobox datetime.js arms unless the caller passes Plain. Type \"tomorrow\" into it.",
					Wrap:   wrapForm,
					States: dateStates("field-date", "2026-09-04", "The day it goes live."),
				},
				{
					Name:   "field-time",
					Blurb:  "The same envelope around a native time input. The enhancement reads a clock rather than a calendar.",
					Wrap:   wrapForm,
					States: timeStates(),
				},
				{
					Name:   "field-datetime",
					Blurb:  "A date and a time in one input, one value.",
					Wrap:   wrapForm,
					States: dateStates("field-datetime", "2026-09-04T19:30", "When it goes live."),
				},
				{
					Name:  "field-daterange",
					Blurb: "A start and an end as one labelled. Names must be unique since IDs are derived.",
					Wrap:  wrapForm,
					States: []sample{
						{State: "Date and time, seeded", Data: map[string]any{
							"Legend": "When", "Seed": "session",
							"Start": map[string]any{"Name": "dr_starts_at", "Label": "Starts", "Value": "2026-09-04T19:30"},
							"End":   map[string]any{"Name": "dr_ends_at", "Label": "Ends"},
						}, Note: "Commit the start with the end still empty and the browser fills the end in an hour later. Nothing is seeded server-side."},
						{State: "Dates only, with an error on the end", Data: map[string]any{
							"Legend": "Booking", "Kind": "date",
							"Start": map[string]any{"Name": "dr_from", "Label": "From", "Value": "2026-09-04"},
							"End": map[string]any{"Name": "dr_to", "Label": "To", "Value": "2026-09-01",
								"Error": "The end comes before the start."},
						}},
					},
				},
			},
		},
		{
			Key:   "route",
			Title: "Route",
			Blurb: "The pieces that are a whole response rather than a piece of one.",
			Partials: []partialDoc{
				{
					Name:   "error-page",
					Blurb:  "The whole body of an error response: the status, one honest sentence, a way back, and 500 references an admin can use.",
					States: errorPageStates(),
				},
				{
					Name:  "back-nav",
					Blurb: "The link back up one level, above a show or edit screen.",
					States: []sample{
						{State: "Back to a record", Data: map[string]any{"Href": "/orders/1", "Label": "Order AB3PX"}},
					},
				},
				{
					Name:  "locale-menu",
					Blurb: "The language switcher: a details menu of one-field POST forms to /_locale, so the choice lands in the locale cookie and the reader stays on the same path.",
					States: []sample{
						{State: "Twelve languages, current highlighted.", Build: localeMenuData,
							Note: "This one posts to /_locale, which would require a server. The switcher in this page's own header is the links-only version."},
					},
				},
			},
		},
	}
}

func statusPillStates() []sample {
	labels := map[string]string{
		"neutral": "Draft", "positive": "Published", "warning": "Scheduled", "negative": "Failed",
	}
	out := []sample{{State: "Default tone (neutral)", Data: map[string]any{"Label": "Draft"}}}
	for _, tone := range tones {
		out = append(out, sample{State: tone, Data: map[string]any{"Tone": tone, "Label": labels[tone]}})
	}
	return out
}

func badgeStates() []sample {
	labels := map[string]string{
		"neutral": "Internal", "positive": "Live", "warning": "Beta", "negative": "Sold out",
	}
	out := []sample{{State: "No tone (quiet default)", Data: map[string]any{"Label": "Draft"}}}
	for _, tone := range tones {
		out = append(out, sample{State: tone, Data: map[string]any{"Tone": tone, "Label": labels[tone]}})
	}
	return out
}

func meterStates() []sample {
	return []sample{
		{State: "Empty", Data: map[string]any{"Percent": 0, "Text": "0/500"}},
		{State: "Part way", Data: map[string]any{"Percent": 42, "Text": "210/500"}},
		{State: "Nearly full", Data: map[string]any{"Percent": 82, "Text": "412/500"}},
		{State: "Full", Data: map[string]any{"Percent": 100, "Text": "500/500"}},
		{State: "Over budget", Data: map[string]any{"Percent": 140, "Text": "700/500"},
			Note: "The fill is clamped to 100% in the partial."},
	}
}

func calloutStates() []sample {
	bodies := map[string][2]string{
		"info":     {"", "Posts are visible to members only until the site is published."},
		"positive": {"Payments are connected", "You can take payment for tickets now."},
		"warning":  {"Connect payments to start selling", "Your event is live but cannot take payment yet."},
		"negative": {"The last import failed", "Nothing was changed. Fix row 91 and try again."},
	}
	order := []string{"info", "positive", "warning", "negative"}
	out := []sample{{State: "Default tone (info), no title", Data: map[string]any{"Body": bodies["info"][1]}}}
	for _, tone := range order {
		b := bodies[tone]
		data := map[string]any{"Tone": tone, "Body": b[1]}
		if b[0] != "" {
			data["Title"] = b[0]
		}
		out = append(out, sample{State: tone, Data: data})
	}
	out = append(out, sample{State: "negative, announced as an alert", Data: map[string]any{
		"Tone": "negative", "Alert": true,
		"Title": "The import stopped", "Body": "Nothing was changed. Fix row 91 and try again.",
	}, Note: "Alert adds role=\"alert\" to display important messages."})
	return out
}

func fieldStates() []sample {
	return []sample{
		{State: "Bare", Data: map[string]any{"ID": "f_bare", "Name": "f_bare", "Label": "Title"}},
		{State: "With a hint", Data: map[string]any{
			"ID": "f_hint", "Name": "f_hint", "Label": "Slug", "Hint": "lowercase, no spaces",
		}},
		{State: "With help under the control", Data: map[string]any{
			"ID": "f_help", "Name": "f_help", "Label": "Email", "Type": "email",
			"Help": "We only use this to send you replies.", "Autocomplete": "email",
		}},
		{State: "Required", Data: map[string]any{
			"ID": "f_required", "Name": "f_required", "Label": "Full name", "Required": true,
		}},
		{State: "With an error", Data: map[string]any{
			"ID": "f_error", "Name": "f_error", "Label": "Email", "Type": "email",
			"Value": "grace@", "Error": "That does not look like an email address.",
		}},
		{State: "Short", Data: map[string]any{
			"ID": "f_short", "Name": "f_short", "Label": "Postcode", "Short": true, "Value": "D02 XY45",
		}},
	}
}

func fieldTextStates() []sample {
	return []sample{
		{State: "Bare", Data: map[string]any{"Name": "ft_bare", "Label": "Title"}},
		{State: "With a hint", Data: map[string]any{
			"Name": "ft_hint", "Label": "Headline", "Hint": "Shown in the list.",
		}},
		{State: "Required", Data: map[string]any{
			"Name": "ft_required", "Label": "Display name", "Required": true, "Autocomplete": "nickname",
		}},
		{State: "With an error", Data: map[string]any{
			"Name": "ft_error", "Label": "Title", "Value": "Release notes",
			"Error": "A post with that title already exists.",
		}},
	}
}

func fieldSelectStates() []sample {
	few := selectOptions("Admin", "Editor", "Member")
	many := selectOptions(
		"Dublin", "Lisbon", "Madrid", "Berlin", "Rome", "Athens",
		"Helsinki", "Reykjavík", "Warsaw", "Zagreb", "Tallinn", "Valletta",
	)
	return []sample{
		{State: "Three options: a native select", Data: map[string]any{
			"ID": "sel_few", "Name": "sel_few", "Label": "Role", "Options": few,
		}},
		{State: "Twelve options: the filterable combobox", Data: map[string]any{
			"ID": "sel_many", "Name": "sel_many", "Label": "City", "Options": many,
			"Help": "Start typing to filter.",
		}, Note: "Past ten options the partial arms select.js. Below ten it does not: search over a handful of items is furniture, not help."},
		{State: "Twelve options, Plain — the enhancement opted out", Data: map[string]any{
			"ID": "sel_plain", "Name": "sel_plain", "Label": "City", "Options": many, "Plain": true,
		}},
		{State: "Required, with an error", Data: map[string]any{
			"ID": "sel_error", "Name": "sel_error", "Label": "Role", "Options": few, "Required": true,
			"Error": "Pick a role for this person.",
		}},
		{
			State: "Hand-written, with optgroups",
			Note:  "Options here is a flat list, so grouped choices are markup the app writes. select.js renders the groups rather than dropping them; a hand-written select can also refuse the enhancement outright with data-rst-select=\"false\".",
			Raw: `<div class="rst-field"><label class="rst-field__label" for="sel_grouped">Region</label>
<select class="rst-input" id="sel_grouped" name="sel_grouped" data-rst-select data-rst-select-filter="{{T "rastrillo.ui.select_filter"}}" data-rst-select-results="{{T "rastrillo.ui.select_results"}}" data-rst-select-result-one="{{T "rastrillo.ui.select_result_one"}}">
<optgroup label="Europe"><option value="dublin" selected>Dublin</option><option value="lisbon">Lisbon</option><option value="warsaw">Warsaw</option></optgroup>
<optgroup label="Americas"><option value="montreal">Montréal</option><option value="lima">Lima</option><option value="austin">Austin</option></optgroup>
<optgroup label="Asia"><option value="osaka">Osaka</option><option value="hanoi">Hanoi</option><option value="dhaka">Dhaka</option></optgroup>
<optgroup label="Africa"><option value="accra">Accra</option><option value="nairobi">Nairobi</option><option value="tunis">Tunis</option></optgroup>
</select>
</div>`,
		},
	}
}

// dateStates covers field-date and field-datetime, which take the same
// keys and differ only in the value format.
func dateStates(name, value, hint string) []sample {
	return []sample{
		{State: "Enhanced input: type \"tomorrow\", or \"in 3 weeks\"", Data: map[string]any{
			"Name": name + "_live", "Label": "Publish on", "Value": value, "Hint": hint,
		}, Note: "The words it parses ride out on data-rst-date-words, resolved through this page's catalog: the field parses the language the page is in."},
		{State: "Required, with an error", Data: map[string]any{
			"Name": name + "_error", "Label": "Publish on", "Required": true,
			"Error": "Pick a date after today.",
		}},
		{State: "Bounded", Data: map[string]any{
			"Name": name + "_bounded", "Label": "Within this year", "Value": value,
			"Min": minFor(name), "Max": maxFor(name),
		}},
		{State: "Plain — the bare native input", Data: map[string]any{
			"Name": name + "_plain", "Label": "Publish on", "Value": value, "Plain": true,
		}, Note: "No enhancement attributes at all, so datetime.js has nothing to find. The native picker still opens."},
	}
}

func minFor(name string) string {
	if name == "field-datetime" {
		return "2026-01-01T00:00"
	}
	return "2026-01-01"
}

func maxFor(name string) string {
	if name == "field-datetime" {
		return "2026-12-31T23:59"
	}
	return "2026-12-31"
}

func timeStates() []sample {
	return []sample{
		{State: "Enhanced input: type \"half seven\", or \"noon\"", Data: map[string]any{
			"Name": "time_live", "Label": "Doors open", "Value": "19:30", "Hint": "Local time.",
		}},
		{State: "Required, with an error", Data: map[string]any{
			"Name": "time_error", "Label": "Doors open", "Required": true,
			"Error": "Pick a time after the sound check.",
		}},
		{State: "Bounded", Data: map[string]any{
			"Name": "time_bounded", "Label": "Between nine and eleven", "Value": "19:30",
			"Min": "09:00", "Max": "23:00",
		}},
		{State: "Plain — the bare native input", Data: map[string]any{
			"Name": "time_plain", "Label": "Doors open", "Value": "19:30", "Plain": true,
		}},
	}
}

func errorPageStates() []sample {
	out := []sample{}
	for _, s := range []struct {
		Status int
		Note   string
		Data   map[string]any
	}{
		{404, "", map[string]any{"Status": 404, "HomeHref": "/"}},
		{403, "", map[string]any{"Status": 403, "HomeHref": "/", "BackHref": "/posts"}},
		{422, "", map[string]any{"Status": 422, "HomeHref": "/", "BackHref": "/posts/1/edit"}},
		{500, "A 500 is the one status with a reference: an operator can grep the logs for it.",
			map[string]any{"Status": 500, "HomeHref": "/", "Ref": "k3f9tq"}},
		{503, "", map[string]any{"Status": 503, "HomeHref": "/"}},
	} {
		out = append(out, sample{
			State: statusLabel(s.Status), Data: s.Data, Note: s.Note,
		})
	}
	out = append(out, sample{
		State: "Anything else — the generic pair",
		Data:  map[string]any{"Status": 418, "HomeHref": "/"},
		Note:  "Five statuses are worded in the framework catalog. A sixth falls back to the generic pair rather than rendering a missing key's name at a reader.",
	})
	return out
}

func statusLabel(status int) string {
	switch status {
	case 404:
		return "404 — not found"
	case 403:
		return "403 — forbidden"
	case 422:
		return "422 — unprocessable"
	case 500:
		return "500 — server error"
	case 503:
		return "503 — unavailable"
	}
	return "Other"
}

// localeMenuData is locale-menu's Items for one page: every locale the
// framework ships, autonym from that locale's own catalog, the page's
// own marked current. Href goes unread by the partial (each item is a
// POST form, not a link) but is set anyway, so the value is a complete
// rastrillo.LocaleItem rather than a half-filled one.
func localeMenuData(current string) any {
	catalogs := rastrillo.BaseCatalogs()
	items := make([]rastrillo.LocaleItem, 0, len(rastrillo.BaseLocales()))
	for _, code := range rastrillo.BaseLocales() {
		items = append(items, rastrillo.LocaleItem{
			Code:    code,
			Name:    catalogs[code]["rastrillo.ui.locale_name"],
			Href:    "/" + code + "/",
			Current: code == current,
		})
	}
	// "/" rather than the gallery's own mount: the mount is an argument
	// to Render now, and a fixture that spelled it would put a URL of
	// whoever published the tree into a sample meant to show the partial.
	// The other fake URLs in this file (/posts/1/edit, /orders/AB3PX)
	// are site-neutral for the same reason.
	return map[string]any{"Items": items, "Return": "/"}
}
