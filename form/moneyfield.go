package form

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo/currency"
)

// The catalog keys a currency-aware Money field reports. Keys, not
// English: the field is read with no request translator in reach, and
// the renderer resolves each with T (or Tf with ErrorArgs).
const (
	moneyInvalidKey            = "rastrillo.ui.money_invalid"
	moneyPrecisionKey          = "rastrillo.ui.money_precision" // 2 places
	moneyPrecision0Key         = "rastrillo.ui.money_precision_0"
	moneyPrecision3Key         = "rastrillo.ui.money_precision_3"
	moneyDecimalCommaKey       = "rastrillo.ui.money_decimal_comma"
	moneyDecimalPointKey       = "rastrillo.ui.money_decimal_point"
	moneyCurrencyKey           = "rastrillo.ui.money_currency"
	moneyNegativeKey           = "rastrillo.ui.money_negative"
	moneyReenterKey            = "rastrillo.ui.money_reenter"
	moneyCurrencyChangedKey    = "rastrillo.ui.money_currency_changed"
	moneyWrongCurrencyKey      = "rastrillo.ui.money_wrong_currency"
	moneyAmbiguousKey          = "rastrillo.ui.money_ambiguous"
	moneyAmbiguousPrecisionKey = "rastrillo.ui.money_ambiguous_precision"
)

// MoneyInput is what a money input renders and a failed POST echoes:
// the text, the locale it is written in, the currency it is in, and the
// seed. ui's field-money partial takes the four as Value, Locale,
// Currency and Seed, and renders the last three as the hidden inputs
// <Name>.locale, <Name>.currency and <Name>.seed.
type MoneyInput struct {
	Value, Locale, Currency, Seed string
}

// SeedMoney is the input for a stored amount, or for none (minor nil:
// a create form, or an empty field). Value is the amount as the parser
// reads it back in renderLocale. Seed is the same text on a normalising
// field, so the POST can tell the person left it alone; a strict
// field's reading of its own seed is exact, so it needs none.
//
// Locale is the id renderLocale resolves to, never renderLocale as
// given: the POST accepts only a shipped id spelled the way Resolve
// spells it, so "de-DE" written into the form would ask for the amount
// again on every save.
func SeedMoney(renderLocale string, minor *int64, code string, strict bool) (MoneyInput, error) {
	c, err := currency.Normalize(code)
	if err != nil {
		return MoneyInput{}, err
	}
	in := MoneyInput{Locale: currency.Resolve(renderLocale).ID, Currency: c}
	if minor == nil {
		return in, nil
	}
	if in.Value, err = currency.InputValue(in.Locale, *minor, c); err != nil {
		return MoneyInput{}, err
	}
	if !strict {
		in.Seed = in.Value
	}
	return in, nil
}

// Minor is a currency-aware Money field's amount in its currency's
// minor unit. present is false for a blank field and for a field with
// an error, so a handler that stores only present amounts cannot store
// a refused one.
func (p *Parsed) Minor(name string) (minor int64, present bool) {
	return p.minor[name], p.present[name]
}

// ErrorArgs is, per field, the values its error message interpolates:
// {currency} for money_wrong_currency, {a} and {b} for money_ambiguous,
// all three for money_ambiguous_precision. A renderer passes them to
// Tf with the key. Fields whose message takes none are absent.
func (p *Parsed) ErrorArgs() map[string]map[string]any { return p.args }

// MoneyInput is the re-render of a submitted money field: the person's
// own text, unchanged, in the locale it was read in, beside the
// server's currency. The seed comes back only when this submission
// verified it (the text was the seed and the record still renders to
// it), so an unchanged ambiguous amount survives an error on another
// field; the next POST verifies it against the record again.
//
// reseed is true when the field's locale metadata or seed was refused:
// the text's meaning is unknown, so it is not echoed, and the caller
// seeds the input from the server instead (blank on create, SeedMoney
// of the record on edit). The MoneyInput returned then carries only the
// render locale and the currency, which is the create form's blank.
func (p *Parsed) MoneyInput(name, renderLocale string) (in MoneyInput, reseed bool) {
	locale, read := p.echoLocale[name]
	if !read || p.reseed[name] {
		return MoneyInput{Locale: currency.Resolve(renderLocale).ID, Currency: p.moneyCurrency[name]}, true
	}
	return MoneyInput{Value: p.echo[name], Locale: locale, Currency: p.moneyCurrency[name], Seed: p.verifiedSeed[name]}, false
}

// parseMoney is a currency-aware Money field's whole POST path, in the
// prototype's order (spec §5.3): the locale echo, blank, the currency
// echo, the unchanged seed, the reading Strict selects, the negative
// policy (for a reading, not for a verified seed). Each check that fails stops there, with the one key that
// says what to do.
func (p *Parsed) parseMoney(r *http.Request, f Field) {
	code, err := currency.Normalize(f.Currency)
	if err != nil {
		// A field's currency is the server's own value (the resource
		// default or the record's). A bad one is a programming error,
		// and failing loudly beats storing an amount in no currency.
		panic(fmt.Sprintf("form: field %q: currency %q: %v", f.Name, f.Currency, err))
	}
	raw := r.PostFormValue(f.Name)
	p.echo[f.Name] = raw
	p.moneyCurrency[f.Name] = code
	// Resolved even without the echo, so a MoneyInput built from it
	// names a locale the next POST's check accepts.
	locale := currency.Resolve(f.Locale).ID
	if f.LocaleEcho {
		v, ok := oneValue(r, f.Name+".locale")
		if !ok || !validLocaleEcho(v) {
			p.errs[f.Name] = moneyReenterKey
			p.reseed[f.Name] = true
			return
		}
		locale = v
	}
	p.echoLocale[f.Name] = locale
	if currency.Clean(raw) == "" {
		if f.Required {
			p.errs[f.Name] = fieldRequiredKey
		}
		return
	}
	if f.LocaleEcho {
		v, ok := oneValue(r, f.Name+".currency")
		if n, err := currency.Normalize(v); !ok || err != nil || n != code {
			// Compared, then discarded: the stored currency is always
			// the server's, so a forged value can only cause this
			// rejection, never a different denomination.
			p.errs[f.Name] = moneyCurrencyChangedKey
			return
		}
	}
	var minor int64
	seeded := false
	// Spec §5.3 describes the seed check on LocaleEcho fields, but it
	// runs without the echo too: field-money always posts a seed, and a
	// hand-written edit form that seeds with SeedMoney would otherwise
	// save an unchanged en "1.500" KWD once and refuse it as ambiguous
	// after any error. It can only refuse or keep the stored amount, so
	// running it costs a caller that sends no seed nothing.
	seed, _ := oneValue(r, f.Name+".seed") // absent, repeated and empty are the same: no seed
	switch {
	case !f.Strict && seed != "" && f.Stored == nil:
		// A create form, or an empty field, never renders a seed.
		p.errs[f.Name] = moneyReenterKey
		p.reseed[f.Name] = true
		return
	case !f.Strict && seed != "" && strings.TrimSpace(raw) == seed:
		// The person left the rendered text alone, so the parser is not
		// run: an ambiguous seed such as en "1.500" for 1.5 KWD saves
		// unchanged. The seed is never a value. The record must still
		// render to exactly this seed, or it changed since the form was
		// drawn (or the seed was forged): re-enter, store nothing.
		if want, _ := currency.InputValue(locale, *f.Stored, code); want != seed {
			p.errs[f.Name] = moneyReenterKey
			p.reseed[f.Name] = true
			return
		}
		minor, seeded = *f.Stored, true
		p.verifiedSeed[f.Name] = seed // echoed if the form fails elsewhere; re-verified on the next POST
	default:
		read := currency.ParseNormalized
		if f.Strict {
			read = currency.Parse
		}
		var err error
		if minor, err = read(locale, raw, code); err != nil {
			var amb *currency.AmbiguousError
			switch {
			case errors.As(err, &amb):
				p.errs[f.Name], p.args[f.Name] = ambiguity(amb, locale, code)
			case errors.Is(err, currency.ErrWrongCurrency):
				p.errs[f.Name] = moneyWrongCurrencyKey
				p.args[f.Name] = map[string]any{"currency": code}
			default:
				p.errs[f.Name] = moneyErrorKey(err, code)
			}
			return
		}
	}
	// The policy is for what the person typed. A verified seed is the
	// record's own amount, so a stored refund (a legacy row imported
	// through [legacy] into a field that never allows negatives) saves
	// unchanged, and editing the record's other fields still works.
	if minor < 0 && !f.Negative && !seeded {
		p.errs[f.Name] = moneyNegativeKey
		return
	}
	p.minor[f.Name], p.present[f.Name] = minor, true
}

// ambiguity is the message for input with two readings. Where one of
// them is over-precise ("1.5 JPY"), money_ambiguous would offer an
// amount that cannot exist, so money_ambiguous_precision names it as
// {a} and says why it is out, and offers {b}.
func ambiguity(amb *currency.AmbiguousError, locale, code string) (string, map[string]any) {
	lo, hi := amb.Choices(locale, code)
	switch {
	case !amb.Readings[0].Storable:
		return moneyAmbiguousPrecisionKey, map[string]any{"a": lo, "b": hi, "currency": code}
	case !amb.Readings[len(amb.Readings)-1].Storable:
		return moneyAmbiguousPrecisionKey, map[string]any{"a": hi, "b": lo, "currency": code}
	}
	return moneyAmbiguousKey, map[string]any{"a": lo, "b": hi}
}

// oneValue is the single submitted value of name. Absent or repeated
// is refused: two locale values would let the client choose which one
// the server reads.
func oneValue(r *http.Request, name string) (string, bool) {
	vs := r.PostForm[name]
	if len(vs) != 1 {
		return "", false
	}
	return vs[0], true
}

// validLocaleEcho is the shape check from spec §5: at most 35 bytes of
// [A-Za-z0-9-], and exactly a shipped id as Resolve spells it, so the
// echo can only name a locale this binary would itself have rendered.
func validLocaleEcho(v string) bool {
	if v == "" || len(v) > 35 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-') {
			return false
		}
	}
	return currency.Resolve(v).ID == v
}

// moneyErrorKey maps the parsers' remaining errors to keys. Three
// precision keys suffice because the storage set's exponents are 0, 2
// and 3 only. A code with another exponent would get the 2-place
// message here, so TestPrecisionKeysCoverEveryExponent fails on one
// first and asks for its own approved key.
func moneyErrorKey(err error, code string) string {
	switch {
	case errors.Is(err, currency.ErrPrecision):
		switch exp, _ := currency.Exponent(code); exp {
		case 0:
			return moneyPrecision0Key
		case 3:
			return moneyPrecision3Key
		default:
			return moneyPrecisionKey
		}
	case errors.Is(err, currency.ErrDecimalComma):
		return moneyDecimalCommaKey
	case errors.Is(err, currency.ErrDecimalPoint):
		return moneyDecimalPointKey
	case errors.Is(err, currency.ErrCurrencySign):
		return moneyCurrencyKey
	}
	return moneyInvalidKey
}
