# 🤖 number

`amadan.net/rastrillo/rastrillo/number`

Writes and reads plain numbers in a reader's language, from the same
CLDR packs as [currency](/docs/reference/currency), so a number drawn on
a page is read with the marks it was drawn with. Values are exact decimals, never
floats. [Localization](/docs/localization#numbers) is the guide;
`{{number}}` and `form.Number` call this package for you.

## Decimal

```go
type Decimal string
```

A number written exactly in ASCII: an optional `-`, the whole part with
no leading zeros, and optionally `.` and the fraction, as in `-1200.5`.
It is its own type so a string that only looks like a number, such as a
version, is never formatted as one by accident.

## Format and InputValue

```go
type Options struct {
	Plain   bool
	Places  int
	Percent bool
}

func Format(locale string, value Decimal, options Options) (string, error)
func FormatFloat(locale string, value float64, bits int, options Options) (string, error)
func InputValue(locale string, value Decimal) (string, error)
```

`Format` writes `value` the way `locale` writes numbers: its digits,
decimal mark, group mark and grouping, and its minus sign. `Plain`
leaves the group marks out. `Places` pads the fraction to that many
digits and never rounds a longer one. `Percent` writes the value with
the locale's percent pattern, as shown: 50 is `50 %` in German.

| `locale` | `value` | `Options` | `Format` |
|---|---|---|---|
| `en` | `1234567.5` | | `1,234,567.5` |
| `de` | `1234567.5` | | `1.234.567,5` |
| `es` | `1234` | | `1234` |
| `fr` | `1234` | | `1 234`, a narrow no-break space |
| `hi` | `1234567` | | `12,34,567` |
| `de` | `1234567.5` | `Plain` | `1234567,5` |
| `en` | `1200.5` | `Places: 2` | `1,200.50` |
| `de` | `50` | `Percent` | `50 %` |

A `value` that is not a `Decimal` is an error that shows the form to
use.

`FormatFloat` writes a float as its shortest exact decimal (`bits` is 32
for a `float32`), and NaN and the infinities in the locale's own symbols
and pattern, as ICU does: `−∞` in Swedish, `epäluku` in Finnish, `∞ %`
as a German percent. `{{number}}` uses it for floats.

`InputValue` is the text an input starts from: the locale's digits and
decimal mark, an ASCII minus and no group marks, so no reading in its
own locale takes it for a thousand times more. `Parse` reads it back as the same value.
`ParseNormalized` refuses one with at most three whole digits and
exactly three fraction digits (German `1,234`) as ambiguous, which is
why `form.Number` keeps an unchanged seed unread.

## Reading what someone typed

```go
func ParseNormalized(locale, text string) (Decimal, error)
func Parse(locale, text string) (Decimal, error)

const MaxDigits = 30

var ErrInvalid = errors.New("number_invalid")
var ErrTooLong = errors.New("number_too_long")

type AmbiguousError struct {
	Readings []Decimal
}

func (ambiguity *AmbiguousError) Choices(locale string) (smallest, largest string)
func (ambiguity *AmbiguousError) Error() string
```

`ParseNormalized` reads a number the way a money field reads an amount,
with no currency. Text with one plausible reading is taken. Where the
page's locale takes every mark for a group mark, that reading wins:
`1,500` is 1500 in English and `1.500` is 1500 in German. Text whose
readings still disagree, such as `1.500` in English, is an
`*AmbiguousError`, whose `Choices` writes the smallest and largest
reading so neither can be taken for the other. Its `Error` is
`number_ambiguous`, the catalog key's suffix.

`Parse` is the strict reading: the locale's own decimal mark, its own
group mark only where its grouping puts it, and a minus as the only
sign.

`ParseNormalized` takes a leading `+`, `-` or `−`, a trailing `-` or
`−`, or parentheses as the sign; `Parse` a leading or trailing minus.
Both return the canonical `Decimal`. Exponents,
`NaN`, letters, two decimal marks and misplaced group marks are
`ErrInvalid`. More than `MaxDigits` digits as typed, leading and
trailing zeros included, is `ErrTooLong`. Blank text
is `""` with no error.
