# 🤖 locales

`amadan.net/rastrillo/rastrillo/locales`

The CLDR data that writes and reads money in each language. The core
is this package. Each language's data is a pack of its own,
`locales/<lang>`, which your app links with a blank import. Go links
only what is imported, so a language you do not offer costs nothing.
[Localization](/docs/localization) is the guide.

## Packs

One pack covers one base language and every modern regional variant of
it: `locales/de` has `de`, `de-AT`, `de-CH`, `de-LI` and the rest. A
generated app gets the packs it needs from `manifest/app.toml`.
`rastrillo generate` writes them to `gen/locales/packs.go`:

```go
import (
	_ "amadan.net/rastrillo/rastrillo/locales/en"
)
```

A hand-written app imports the packs for the languages it offers. If one
is missing, the app refuses to start and names the import to add.

```go
func PacksFor(offered []string) ([]string, error)
```

`PacksFor` is the packs a set of language codes needs, without
duplicates: `en`, `zh-Hans`, `zh-Hant`, `pt-PT` and `yue` need `en`,
`pt`, `yue` and `zh`. A code is what your app offers, routes and keeps
catalogs under; a pack is only where its formatting data lives.

The `en` pack adds about 55 KB to a binary, and most packs far less.
`locales/modern` imports all 101, about 500 KB, for a tool that has to
format in any language.

## CheckConfig

```go
func CheckConfig(defaultLocale string, offered []string) (Config, error)
func CanonicalIdentity(code string) (string, error)

type Config struct {
	Default       string            // the declared effective default: an offered identity
	DefaultFormat string            // the linked locale that formats it
	Offered       []string          // as declared, canonical
	Formats       map[string]string // offered identity -> formatting locale
	Packs         []string          // derived, deduplicated
	LinkedPacks   []string          // what this binary actually links
}
```

`CheckConfig` checks the languages an app offers, in the binary that
will serve them. `Serve` and `Handler` run it before they open the
database, and the app refuses to start on any problem. It checks that:

- each code is written the way CLDR writes it: no spaces, hyphens not
  underscores, the language in lower case, a script in title case and a
  region in upper case. `CanonicalIdentity` checks one code and returns
  the right spelling;
- no code is offered twice;
- each code is a language CLDR's modern set formats, and its pack is
  linked. A parent standing in for a missing pack is not enough;
- the default is one of the offered codes. It is `defaultLocale`, or
  the first offered code, or `en`.

A wrong configuration is never repaired, because a code that only works
once corrected would be stored and routed under its wrong spelling. The
error says what to write:

```text
locale configuration: "en_US" is not in canonical form; write "en-US"
locale configuration: "de" needs the de pack: import _ "amadan.net/rastrillo/rastrillo/locales/de"
```

`Config` keeps your codes and their formatting locales apart. `zh-TW`
is the code your app routes and keeps catalogs under; `Formats` says
`zh-Hant` writes its money.

## Resolve

```go
func Resolve(tag string) Resolution

type Resolution struct {
	ID          string
	Wanted      string
	MissingPack string
	Malformed   bool
	UsedDefault bool
}
```

`Resolve` finds the linked data for any language tag, such as one from
`Accept-Language` or a stored preference. Unlike `CheckConfig`, it is
forgiving. It checks the tag's grammar, applies CLDR's aliases and
likely scripts, and matches every locale rastrillo ships, linked or not.
That match is `Wanted`. `zh-TW` is not a CLDR locale, so it matches
`zh-Hant`; cutting it to `zh` would pick Simplified Chinese data.

If `Wanted`'s pack is not linked, `Resolve` walks up CLDR's parents to a
linked one, and names the pack it lacked in `MissingPack`: `hi-Latn`
falls back to `en-IN`, `ht` to `fr-HT`, `nb` and `nn` to `no`. With no
linked parent it uses the app's default language and sets
`UsedDefault`. `ID` is the locale that writes the amount. Only the
conventions fall back; the currency never changes.

`Malformed` is a tag that is not a well-formed language tag, such as
`de--CH`.

## The configuration probe

Run your app with `RASTRILLO_CHECK_CONFIG=1` and it checks its
languages, prints one JSON line and exits. It opens no database, runs no
migration and serves nothing, so it is safe to run beside a live copy:

```sh
RASTRILLO_CHECK_CONFIG=1 go run ./cmd/shop
```

```text
{"rastrillo_check_config":1,"ok":true,"default":"en","default_format":"en","offered":[],"formats":{"en":"en"},"packs_required":["en"],"packs_linked":["en"],"problems":[]}
RASTRILLO_CHECK_CONFIG=1: checked the configuration and exited without serving; unset it to serve
```

It exits 0 when the configuration is good and 1 when `problems` lists
something. Any other value of the variable exits 2:

```text
RASTRILLO_CHECK_CONFIG="yes": set it to 1 to check the configuration, or unset it to serve
```

It always exits, even in production. A server that inherits the
variable stops at once, with that line in its log, rather than serving.

`rastrillo.Run` and `rastrillo.Resolve` run the probe first. A `main`
that calls neither calls
[`rastrillo.ProbeConfig`](/docs/reference/rastrillo#entrypoints) first.
`rastrillo doctor` builds your app and runs the probe for you; see
[The CLI](/docs/cli#templates-and-locales).

```go
func Probe(env, defaultLocale string, offered []string, stdout, stderr io.Writer) (run bool, exit int)

type ProbeReport struct {
	Version       int               `json:"rastrillo_check_config"`
	OK            bool              `json:"ok"`
	Default       string            `json:"default"`
	DefaultFormat string            `json:"default_format"`
	Offered       []string          `json:"offered"`
	Formats       map[string]string `json:"formats"`
	PacksRequired []string          `json:"packs_required"`
	PacksLinked   []string          `json:"packs_linked"`
	Problems      []string          `json:"problems"`
}
```

`Probe` is the logic `ProbeConfig` runs. `ProbeReport` is the line it
prints; every field is always there, so a script can read it.

## The rest

Most apps never call these. The framework uses them, and so can a test
or a tool.

```go
func Install(cfg Config)
func SetDefault(id string)
func SetDefaultForTest(id string) (restore func())
func FormatFor(identity string) string
```

`Install` makes an accepted `Config` the process's own, so request-time
formatting uses the mapping boot checked. `rastrillo.Handler` calls it.
`SetDefault` sets only the default formatting locale.
`SetDefaultForTest` replaces the default for one test and returns a
function that puts it back. `FormatFor` is the formatting locale for a
code: the installed mapping when the code is offered, else what
`Resolve` finds.

```go
func Linked(id string) bool
func LinkedIDs() []string
func LinkedPacks() []string
func PackOf(id string) (string, bool)
func Parent(id string) string
func DigitSets() []string
func StorageCodes() []string
func Storage(code string) (Currency, bool)
const CLDRVersion = "48.0.0"
```

`Linked`, `LinkedIDs` and `LinkedPacks` say what this binary links.
`PackOf` names the pack that carries a locale, linked or not. `Parent`
is a locale's CLDR parent, which may be in another pack. `DigitSets` is
every numbering system's digits, so text in any of them can be read.
`StorageCodes` lists the storage set, and `Storage` gives one code's
`Currency`: its `Exponent`, and whether it is `Eligible` for new
records. `CLDRVersion` is the CLDR release the data comes from.

```go
type Locale struct{ /* the marks, digits, patterns and symbols of one locale */ }
type Override struct{ Pattern, Decimal, Group string }
func Data(id string) (Locale, bool)
func Merge(child, parent Locale) Locale
func Register(pack string, locs map[string]Locale)
```

`Locale` is one locale's money data, stored in its pack as what differs
from its parent. `Override` is a pattern or mark that applies to one
currency only. `Data` merges a locale with its parents, and `Merge`
merges one pair. `Register` is how a pack adds its locales when it is
imported; you never call it.

Two test packages sit under `locales/localestest`. `gallery` links only
the twelve languages the framework's strings ship in. `crosspack` links
only `en`, `fr` and `no`, to test the fallbacks across packs.
