// Package dates writes dates and times in a reader's locale from CLDR
// 48's patterns, which each language pack (locales/<lang>) carries
// beside its number and money data: the locale's own order, names and
// clock ("Friday, 20 November", "Freitag, 20. November 2026",
// "9:09 PM"), the first day of its week, and how it writes a date in
// numbers. The browser draws the same patterns with
// browser/dates.mjs (Assets), byte for byte.
//
// Moved from Mapa (internal/mapa/locale, level-up build note 85) with
// its kinds and semantics kept, so that one copy serves every app.
package dates

import (
	"encoding/json"
	"sync"

	"amadan.net/rastrillo/rastrillo/locales"
)

// Kinds are the dates a Locale writes, as Format's kind: the weekday
// with the date ("day", and "dayYear" with the year), the same short
// ("short"), the date alone ("date", "dateYear"), "monthYear",
// "month", "year", a time of day ("time") and a moment in full
// ("stamp").
var Kinds = []string{"day", "dayYear", "short", "date", "dateYear",
	"monthYear", "month", "year", "time", "stamp"}

// field is one part of a pattern: a literal, or a date's field.
type field struct {
	Type  string `json:"t"`
	Value string `json:"v,omitempty"`
	Pad   bool   `json:"pad,omitempty"`
	Cycle string `json:"cycle,omitempty"`
}

// pattern is one kind of date in one locale, with the names it uses
// and, where they are not ASCII's, its digits.
type pattern struct {
	Parts    []field  `json:"parts"`
	Weekdays []string `json:"weekdays,omitempty"`
	Months   []string `json:"months,omitempty"`
	Periods  []string `json:"periods,omitempty"`
	Digits   string   `json:"digits,omitempty"`
}

// record is one locale's date data, as hack/cldrdates writes it.
type record struct {
	FirstDay int    `json:"firstDay"`
	Order    string `json:"order"`
	// Separator is the mark between a numeric date's numbers.
	Separator string              `json:"separator"`
	Weekdays  []string            `json:"weekdays"`
	Narrow    []string            `json:"narrow"`
	Patterns  map[string]*pattern `json:"patterns"`
}

// Locale is one locale's dates. A nil Locale is a locale with no date
// data linked: it writes ISO dates, and weeks that start on Monday.
type Locale struct {
	code string
	data record
}

// decoded holds each linked id's Locale, decoded on first use.
var decoded sync.Map

// For is the dates of the locale tag resolves to, as {{number}} and
// money resolve it: "de-AT" is de-AT's, "en-US" is en's, and a tag
// whose pack is not linked falls to the app's default. It is nil when
// nothing linked covers it, or its dates are not drawn
// (hack/cldrdates/coverage.tsv lists those, with the reason).
func For(tag string) *Locale {
	id := locales.Resolve(tag).ID
	if found, ok := decoded.Load(id); ok {
		return found.(*Locale)
	}
	raw, ok := locales.Dates(id)
	if !ok {
		return nil
	}
	made := &Locale{code: id}
	if err := json.Unmarshal([]byte(raw), &made.data); err != nil {
		// The records are generated and decoded in this package's
		// tests for every shipped locale, so this is a build defect.
		panic("dates: " + id + ": " + err.Error())
	}
	stored, _ := decoded.LoadOrStore(id, made)
	return stored.(*Locale)
}

// Code is the locale id the dates are written in; "" for a nil Locale.
func (in *Locale) Code() string {
	if in == nil {
		return ""
	}
	return in.code
}

// Client is what the browser is given to write dates with
// (browser/dates.mjs's useDates): the patterns a picker, a chip and a
// month's title draw with. Put it in the page as JSON.
type Client struct {
	Code     string              `json:"code"`
	FirstDay int                 `json:"firstDay"`
	Order    string              `json:"order"`
	Weekdays []string            `json:"weekdays"`
	Narrow   []string            `json:"narrow"`
	Patterns map[string]*pattern `json:"patterns"`
}

// clientKinds are the patterns the browser draws with.
var clientKinds = []string{"day", "dayYear", "monthYear"}

// Client is the browser's share of the locale's data; nil for a nil
// Locale, which dates.mjs writes as ISO dates.
func (in *Locale) Client() *Client {
	if in == nil {
		return nil
	}
	out := &Client{Code: in.code, FirstDay: in.data.FirstDay,
		Order: in.data.Order, Weekdays: in.data.Weekdays,
		Narrow: in.data.Narrow, Patterns: map[string]*pattern{}}
	for _, kind := range clientKinds {
		out.Patterns[kind] = in.data.Patterns[kind]
	}
	return out
}
