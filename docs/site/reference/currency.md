# 🤖 currency

`amadan.net/rastrillo/rastrillo/currency`

Formats, seeds and parses money: an integer count of a currency's
smallest unit, with its ISO 4217 code, written in a reader's language
from CLDR data. [Forms](/docs/forms#money) and
[Templates](/docs/templates#group-the-digits-of-a-quantity) are the
guides; `form` and `ui` call this package for you.

## Format and InputValue

```go
func Format(locale string, minor int64, code string) (string, error)
func InputValue(locale string, minor int64, code string) (string, error)
```

`Format` writes an amount the way `locale` writes money: its digits, its
marks, its grouping, and where the symbol goes.

| `locale` | `minor` | `code` | `Format` |
|---|---|---|---|
| `en` | 128450 | `USD` | `$1,284.50` |
| `de` | 128450 | `EUR` | `1.284,50 €` |
| `pt-PT` | 128450 | `EUR` | `1284,50 €` |
| `ja` | 1285 | `JPY` | `￥1,285` |
| `en` | 1500 | `KWD` | `KWD 1.500` |
| `en` | -500 | `USD` | `-$5.00` |

The currency comes from `code`, never from `locale`. Every digit of the
currency's smallest unit is shown, so nothing is rounded away. A code
outside the storage set is an error.

`InputValue` is the text to put in an input: the language's digits and
decimal mark, no group marks, no symbol. 150000 USD is `1500.00` in
`en`, and 128450 EUR is `1284,50` in `de`. The parser reads it back as
the same amount.

## Codes

```go
func Normalize(code string) (string, error)
func Exponent(code string) (int, error)
func EligibleDefault(code string) bool
```

`Normalize` trims the code, requires three ASCII letters, upper-cases
it, and requires it to be in the storage set: the 174 codes rastrillo
will store an amount in. Every entry point that takes a code calls it.
A code outside the set is refused, never given a guessed scale: `KDW`, a
typo for `KWD`, would otherwise store fils as cents. See
[money](/docs/reference/money#the-storage-set).

`Exponent` is the code's number of decimal places: 0 for JPY, 2 for
USD, 3 for KWD. It never changes for a code.

`EligibleDefault` reports whether a new record may start in the code.
153 codes can. A code no longer issued, such as HRK, still stores,
formats and edits, but is not a default for new records.

## Parsing

```go
func ParseNormalized(locale, s, code string) (int64, error)
func Parse(locale, s, code string) (int64, error)
func Clean(s string) string
```

`ParseNormalized` is a money field's default reading. It accepts text
with one plausible reading, refuses text that could be two amounts, and
never rounds. [Forms](/docs/forms#what-a-money-field-accepts) has the
rules and a table of examples.

`Parse` is the strict reading, for `Field.Strict`. It takes only
`locale`'s own format: one sign (a leading or trailing `-`, never `+`
or parentheses), at most one marker for the currency (its symbol in
`locale`, or its code), and no more decimals than the currency has.

Both read signs. Whether a negative amount is allowed is the caller's
decision. Both return 0 and no error for blank text.

`Clean` removes direction marks and turns every kind of space into a
plain one, then trims. Both readings start with it, and so does a
required check: text of only spaces or direction marks is blank.

### Text with two readings

```go
type AmbiguousError struct {
	Readings []Reading
}
type Reading struct {
	Decimal  string
	Storable bool
	Minor    int64
}

func (e *AmbiguousError) Choices(locale, code string) (a, b string)
func (e *AmbiguousError) Error() string
```

`ParseNormalized` returns an `*AmbiguousError` for text whose readings
disagree, such as `1.500` on an English page. `Readings` holds each one,
smallest first. `Decimal` is the exact amount; `Storable` is false when
it has more decimals than the currency, and then `Minor` is not set.

`Choices` writes the smallest and largest readings so that no reader
can take one for the other: the ISO code after the amount, no group
marks, and an extra zero where three decimals would look like a group.
`1.500` in a KWD field gives `1.5 KWD` and `1500 KWD`; `1,234` in a EUR
field on a German page gives `1,2340 EUR` and `1234 EUR`. `form` puts
them in the `money_ambiguous` message.

### Errors

```go
var (
	ErrMalformedCurrency   = errors.New("currency code is not three ASCII letters")
	ErrUnsupportedCurrency = errors.New("currency code is not in the storage set")
	ErrNotInteger          = errors.New("amount is not an integer count of minor units")
	ErrOutOfRange          = errors.New("amount does not fit in int64")

	ErrInvalid      = errors.New("money_invalid")
	ErrPrecision    = errors.New("money_precision")
	ErrDecimalComma = errors.New("money_decimal_comma")
	ErrDecimalPoint = errors.New("money_decimal_point")
	ErrCurrencySign = errors.New("money_currency")
)
var ErrWrongCurrency = errors.New("money_wrong_currency")
```

The first four are for you: a bad code, or an amount that is not an
integer that fits. The rest are a person's input refused. Their text is
the `rastrillo.ui.*` catalog key's last part, and `form` turns each into
the message the person sees.

## Rescale and Legacy

```go
func Rescale(v int64, scale int, code string) (int64, error)

type Legacy struct {
	Currency string
	Scale    int
}
```

`Rescale` converts an integer with `scale` decimal places into `code`'s
minor units, exactly or not at all. At scale 2, 1200 is 12 JPY, and 1250
is an error: `1250 at scale 2 is 12.50, which JPY cannot hold`. Nothing
is rounded, because a rounded price is a price nobody chose.

`Legacy` is a manifest's `[legacy]` table, the currency and scale that
amounts stored before currencies existed were written in. The SQL
upgrade and the event replay both read it.

## Resolve and AmountFromAny

```go
func Resolve(tag string) locales.Resolution
func AmountFromAny(v any) (minor int64, present bool, err error)
```

`Resolve` is [`locales.Resolve`](/docs/reference/locales#resolve), here
so a caller handling money needs one import. A money field checks the
language its form sent with `Resolve(v).ID == v`.

`AmountFromAny` is the `money` template function's argument check. It
takes any integer type, or a pointer to one, where a nil pointer is no
amount rather than zero. It refuses floats, strings, and unsigned values
too large for an `int64`.

## A language without its pack

Asked for a language whose pack is not linked, `Format` writes the
amount in the nearest linked parent's conventions, or the app's default
language's, and logs a warning once per pack:

```text
currency: de-AT needs the de pack, which is not linked; amounts are formatted in en instead. Import _ "amadan.net/rastrillo/rastrillo/locales/de"
```

The currency never changes; only how it is written. The languages your
app offers are checked at boot and always linked, so this happens only
for a language your code asks for directly. See
[locales](/docs/reference/locales).

## currency/replay

`amadan.net/rastrillo/rastrillo/currency/replay` holds the money rules
a generated mergeable store replays its events with. You call it only
if you append money events by hand.

Every event a currency-aware store writes carries `"V": 2`, whatever its
kind. An event with no `V` is from before the upgrade, version 1. The
version is checked before anything else, so an event from a newer
version is refused, not guessed at:
`event 4: version 3 is not one this binary understands`.

In a version-2 event, each money field has a currency if and only if it
has an amount. Neither means unchanged. Both set the field. An amount
in a different currency from the one the field already has is refused:
`a currency change needs its own event`. A version-1 amount is rescaled
from the manifest's `[legacy]`.

`Rules.Apply` folds one event, `Replay` a whole stream. A store's reads
and its boot check both go through them, so an event that breaks the
rules is refused on the next read, not only at the next boot.

## currency/sqlmoney

`amadan.net/rastrillo/rastrillo/currency/sqlmoney` upgrades a generated
app's SQL money columns at boot and checks them. `gen.Boot` calls
`Upgrade`; you do not.

`Upgrade` decides what to do from the columns it finds, so a second boot
does nothing. An old column `price` is replaced by `price_minor` and
`price_currency`: the two are added, each amount is copied with
`Rescale` from `[legacy]`, and `price` is dropped. Dropping it rewrites
the table, so the first boot after the upgrade takes longer on a big
table. A money field new to an existing table gets its two columns,
empty in every existing row. Then every money field is checked with
`CheckPairs`: no row may have an amount without a currency, or a
currency without an amount. Any failure rolls everything back, and the
app does not start.
