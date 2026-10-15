package ui

import (
	"context"
	"net/http"
	"reflect"

	"amadan.net/rastrillo/rastrillo"
)

// Localizer is what a template receives as $L: one locale's translator
// and formatting locale. It holds no function map, so nothing is
// rebound per request or per language: the function map is built once,
// and the locale arrives as the locale functions' first argument. A
// rebound map that other requests are executing can put one request's
// locale into another's output with no data race for -race to report.
type Localizer struct {
	identity string
	format   string
	t        func(key string, args ...any) string
}

// NewLocalizer makes the localizer for one offered identity. format is
// the formatting locale locales.CheckConfig chose for it. t must fill
// {name} placeholders from alternating name/value arguments
// (rastrillo.Locales.Tf bound to the identity): T, Tf and Tbdi all go
// through it, and must not be nil.
func NewLocalizer(identity, format string, t func(key string, args ...any) string) *Localizer {
	return &Localizer{identity: identity, format: format, t: t}
}

// Identity is the offered locale code this localizer serves.
func (l *Localizer) Identity() string { return l.identity }

// Dir is the identity's HTML dir attribute, "rtl" or "ltr", so a shell
// lays an Arabic page out right to left without a block override.
func (l *Localizer) Dir() string { return rastrillo.Dir(l.identity) }

// Format is the CLDR locale numbers and money are formatted in.
func (l *Localizer) Format() string { return l.format }

// Localized is what a page needs from its request: the localizer, and
// the language switcher's items and return path. Hand-written view
// structs embed it so templates, and partials given the struct, find
// the localizer under L.
type Localized struct {
	L       *Localizer
	Locales []rastrillo.LocaleItem
	Return  string
}

// Localizer is the localizer the data carries.
func (l Localized) Localizer() *Localizer { return l.L }

// Request returns the request values themselves. It is promoted to
// every struct that embeds Localized, so an app's render function can
// read them off any generated view through
// interface{ Request() ui.Localized } without knowing its type.
func (l Localized) Request() Localized { return l }

// ForRequest is the Localized for r: the localizer WithLocalizer put
// there, the switcher's items, and the URL to come back to.
func ForRequest(r *http.Request) Localized {
	return Localized{L: LocalizerFrom(r), Locales: rastrillo.LocaleItems(r), Return: r.URL.RequestURI()}
}

// localizerKey is unexported so no other package can store a value
// LocalizerFrom would mistake for the request's localizer.
type localizerKey struct{}

// WithLocalizer returns r carrying l.
func WithLocalizer(r *http.Request, l *Localizer) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), localizerKey{}, l))
}

// LocalizerFrom is the localizer WithLocalizer stored, or nil.
func LocalizerFrom(r *http.Request) *Localizer {
	l, _ := r.Context().Value(localizerKey{}).(*Localizer)
	return l
}

var localizerType = reflect.TypeOf((*Localizer)(nil))

// localizer is the template function behind {{$L := localizer $}}: the
// *Localizer under "L" in a map or a struct (embedded fields included),
// or a typed nil. Always the typed pointer, never an untyped nil,
// because the locale functions decide their form by the first
// argument's type: an untyped nil would make a migrated call look like
// a legacy one whose value is nil.
func localizer(data any) *Localizer {
	v := reflect.ValueOf(data)
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer && v.Type() != localizerType) {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	var x reflect.Value
	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() == reflect.String {
			x = v.MapIndex(reflect.ValueOf("L").Convert(v.Type().Key()))
		}
	case reflect.Struct:
		// FieldByName panics when L sits behind a nil embedded pointer
		// (struct{ *Localized }); that data merely lacks a localizer, so
		// it gets the typed nil and strict dispatch reports an error
		// instead of the request dying in a panic.
		if f, ok := v.Type().FieldByName("L"); ok {
			x, _ = v.FieldByIndexErr(f.Index)
		}
	}
	for x.IsValid() && x.Kind() == reflect.Interface && !x.IsNil() {
		x = x.Elem()
	}
	if x.IsValid() && x.Type() == localizerType && x.CanInterface() {
		return x.Interface().(*Localizer)
	}
	return nil
}

// withL passes the localizer into a template call whose data is not a
// dict. A map is copied with "L" added, because the caller's map may be
// shared with other templates or requests and writing into it would
// leak this request's localizer into them. A struct is returned
// unchanged: it must carry its own L (embed Localized), and ui.Pages's
// strict dispatch fails the call that needs it if it does not.
func withL(data any, l *Localizer) any {
	if m, ok := data.(map[string]any); ok {
		out := make(map[string]any, len(m)+1)
		for k, v := range m {
			out[k] = v
		}
		out["L"] = l
		return out
	}
	return data
}
