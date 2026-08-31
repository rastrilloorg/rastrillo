# Base Languages and the Locale Switcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship twelve framework base catalogs, a per-locale fallback level, a language switcher partial backed by a core `POST /_locale` route that writes the locale cookie, and a `--check` rule that fails an app whose non-default catalog leaves `rastrillo.ui.*` keys untranslated.

**Architecture:** The framework's base strings move from a Go map literal into `locales/<code>.toml` files embedded in the root package; `BaseCatalogs()` exposes them per locale and `BaseCatalog()` stays as the `en` view so nothing that calls it today changes. `(*Locales).T` gains one fallback level (framework catalog for the requested locale) between the app default and framework English. The switcher is a `ui` partial rendering one small POST form per locale; `buildHandler` mounts `/_locale` inside the locale middleware. Cookie precedence moves above `Accept-Language` so a switch actually sticks.

**Tech Stack:** Go 1.22+ stdlib (`embed`, `net/http`, `testing/fstest`), `internal/catalog` flat-TOML decoder, `html/template` partials in `ui`, `tokens.css`.

**Spec:** `docs/superpowers/specs/2026-08-28-design-system-design.md` — §0, §2.4, §3. This is PR 1 of §6.

## Global Constraints

- Every user-visible string in a partial is `{{T "rastrillo.ui.<key>"}}`; no hardcoded English (spec §0).
- Every shipped catalog holds exactly the `en` key set (spec §3.2); a gate enforces it.
- The twelve locale codes, verbatim: `en ga zh-Hans es hi pt bn ru ja yue vi ar` (spec §3.1).
- Framework catalogs are matched by the app's **declared** code only — `zh` does not find `zh-Hans` (spec §3.3).
- `go test ./...` must stay green in the root and in `examples/blog`, `examples/tickets`, `examples/notes` (their vendored `tokens.css` pins are byte-identical to `ui.TokensCSS()`; any `tokens.css` change is copied to all three `static/` dirs).
- `SKILL.md` ≤ 18,000 bytes (`TestSkillMDStaysWithinBudget`).
- Build with `GOFLAGS=-mod=mod`. Run root tests as `go test . ./ui/ ./internal/... ./cmd/...`.
- Never merge to main directly: this plan ends in a PR.
- One deliberate deviation from the spec, recorded here: `Dir(locale)` lives in the root `rastrillo` package, not `ui`, because `ui` imports `rastrillo` and `LocaleItems` (which needs it) is in the root. `ui` re-exports nothing.
- One deliberate ruling that changes documented behaviour: cookie precedence rises above `Accept-Language` (Task 6). Without it a switch is undone on the very next unprefixed request, which makes §2.4's switcher decorative.

---

## File map

| File | Responsibility |
|---|---|
| `locales/{en,ga,zh-Hans,es,hi,pt,bn,ru,ja,yue,vi,ar}.toml` | the framework base catalogs (create) |
| `basecatalog.go` | embed the TOML, decode once, expose `BaseCatalog()`, `BaseCatalogs()`, `BaseLocales()`, `BaseKeys()` (rewrite) |
| `basecatalog_test.go` | the key-set gate + existing copy/resolution tests (extend) |
| `locale.go` | `Locales.fw` level, `Dir` (modify) |
| `locale_test.go` | fallback-level and `Dir` tests (extend) |
| `localemw.go` | precedence change, `LocaleItem`, `LocaleItems`, `switchHandler` (modify) |
| `localemw_test.go` | precedence, items, switch route tests (extend) |
| `serve.go` | mount `POST /_locale` (modify `buildHandler`) |
| `serve_router_test.go` | route mounted only with locales (extend) |
| `ui/partials/locale-menu.html` | the switcher partial (create) |
| `ui/tokens.css` | `.rst-locale` idiom (modify) + copy to `examples/*/static/tokens.css` |
| `ui/ui_test.go` | partial in the defined list; render smoke (extend) |
| `internal/generate/locales.go` | `MissingFrameworkKeys` (add) |
| `internal/generate/locales_test.go` | tests (extend) |
| `cmd/rastrillo/generate.go` | wire the new check (modify) |
| `docs/site/localization.md`, `docs/site/templates.md`, `SKILL.md` | docs (modify) |

---

### Task 1: The twelve catalogs and the key-set gate

**Files:**
- Create: `locales/en.toml` … `locales/ar.toml` (12 files)
- Rewrite: `basecatalog.go`
- Modify: `basecatalog_test.go`

**Interfaces:**
- Produces:
  - `func BaseCatalog() Catalog` — unchanged signature; returns a copy of the `en` catalog.
  - `func BaseCatalogs() map[string]Catalog` — copy of every shipped catalog keyed by code.
  - `func BaseLocales() []string` — the twelve codes, `en` first, then the order in spec §3.1.
  - `func BaseKeys() []string` — sorted `en` keys.
  - Keys (all prefixed `rastrillo.ui.`): `pagination search_submit cancel done select_filter select_results select_result_one locale_name shell_skip shell_menu shell_account shell_language`.

- [ ] **Step 1: Write the failing gate test**

Append to `basecatalog_test.go`:

```go
// TestBaseCatalogsShareOneKeySet is spec §3.2's gate: every shipped
// catalog holds exactly the en key set, so a locale can never be
// silently missing a string that en has.
func TestBaseCatalogsShareOneKeySet(t *testing.T) {
	want := []string{"en", "ga", "zh-Hans", "es", "hi", "pt", "bn", "ru", "ja", "yue", "vi", "ar"}
	if got := BaseLocales(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BaseLocales = %v, want %v", got, want)
	}
	all := BaseCatalogs()
	en := all["en"]
	if len(en) == 0 {
		t.Fatal("en catalog is empty")
	}
	for _, code := range want {
		c, ok := all[code]
		if !ok {
			t.Errorf("no catalog for %s", code)
			continue
		}
		for k := range en {
			if v, ok := c[k]; !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s.toml: missing or empty %s", code, k)
			}
		}
		for k := range c {
			if _, ok := en[k]; !ok {
				t.Errorf("%s.toml: key %s is not in en", code, k)
			}
		}
		if code != "en" && c["rastrillo.ui.locale_name"] == en["rastrillo.ui.locale_name"] {
			t.Errorf("%s.toml: locale_name is still English", code)
		}
	}
	for _, k := range BaseKeys() {
		if !strings.HasPrefix(k, "rastrillo.ui.") {
			t.Errorf("key %s is not namespaced rastrillo.ui.*", k)
		}
	}
}

// TestBaseCatalogsAreCopies mirrors TestBaseCatalogIsACopy for the map.
func TestBaseCatalogsAreCopies(t *testing.T) {
	BaseCatalogs()["ga"]["rastrillo.ui.cancel"] = "tampered"
	if got := BaseCatalogs()["ga"]["rastrillo.ui.cancel"]; got == "tampered" {
		t.Fatal("BaseCatalogs returned live maps")
	}
}
```

Add `"reflect"` and `"strings"` to the file's imports.

- [ ] **Step 2: Run to verify it fails**

Run: `GOFLAGS=-mod=mod go test . -run 'TestBaseCatalogs' -v`
Expected: compile error, `undefined: BaseLocales`.

- [ ] **Step 3: Write the twelve catalogs**

Every file starts with the same header comment, then the twelve keys in this order. Values below are the ones to ship; each non-English file's header says it is machine-drafted.

`locales/en.toml`:

```toml
# rastrillo's base strings for en — the source every other catalog is
# checked against (basecatalog_test.go). An app catalog entry for the
# same key wins over this one.
rastrillo.ui.pagination = "Pagination"
rastrillo.ui.search_submit = "Search"
rastrillo.ui.cancel = "Cancel"
rastrillo.ui.done = "Done selecting"
rastrillo.ui.select_filter = "Type to filter"
rastrillo.ui.select_results = "{n} results"
rastrillo.ui.select_result_one = "1 result"
rastrillo.ui.locale_name = "English"
rastrillo.ui.shell_skip = "Skip to content"
rastrillo.ui.shell_menu = "Menu"
rastrillo.ui.shell_account = "Account"
rastrillo.ui.shell_language = "Language"
```

For each other file, header:

```toml
# rastrillo's base strings for <code>. Machine-drafted from en.toml; a
# native speaker's correction is welcome — every key must stay present
# (basecatalog_test.go checks the set, not the wording).
```

Values (key → value), one table per file:

`ga.toml`: Leathanaigh / Cuardaigh / Cealaigh / Roghnú críochnaithe / Clóscríobh chun scagadh / {n} toradh / 1 toradh / Gaeilge / Léim chuig an ábhar / Roghchlár / Cuntas / Teanga

`zh-Hans.toml`: 分页 / 搜索 / 取消 / 完成选择 / 输入以筛选 / {n} 个结果 / 1 个结果 / 简体中文 / 跳到内容 / 菜单 / 账户 / 语言

`es.toml`: Paginación / Buscar / Cancelar / Selección terminada / Escribe para filtrar / {n} resultados / 1 resultado / Español / Ir al contenido / Menú / Cuenta / Idioma

`hi.toml`: पृष्ठांकन / खोजें / रद्द करें / चयन पूर्ण / फ़िल्टर करने के लिए टाइप करें / {n} परिणाम / 1 परिणाम / हिन्दी / सामग्री पर जाएँ / मेनू / खाता / भाषा

`pt.toml`: Paginação / Pesquisar / Cancelar / Seleção concluída / Escreva para filtrar / {n} resultados / 1 resultado / Português / Ir para o conteúdo / Menu / Conta / Idioma

`bn.toml`: পৃষ্ঠা বিন্যাস / খুঁজুন / বাতিল / নির্বাচন সম্পন্ন / ফিল্টার করতে টাইপ করুন / {n}টি ফলাফল / ১টি ফলাফল / বাংলা / মূল বিষয়ে যান / মেনু / অ্যাকাউন্ট / ভাষা

`ru.toml`: Навигация по страницам / Поиск / Отмена / Выбор завершён / Введите текст для фильтра / Результатов: {n} / 1 результат / Русский / Перейти к содержимому / Меню / Аккаунт / Язык

`ja.toml`: ページ送り / 検索 / キャンセル / 選択完了 / 入力して絞り込む / {n} 件 / 1 件 / 日本語 / 本文へ移動 / メニュー / アカウント / 言語

`yue.toml`: 分頁 / 搜尋 / 取消 / 揀完喇 / 輸入嚟篩選 / {n} 個結果 / 1 個結果 / 廣東話 / 跳到內容 / 選單 / 帳戶 / 語言

`vi.toml`: Phân trang / Tìm kiếm / Hủy / Chọn xong / Nhập để lọc / {n} kết quả / 1 kết quả / Tiếng Việt / Đi tới nội dung / Trình đơn / Tài khoản / Ngôn ngữ

`ar.toml`: ترقيم الصفحات / بحث / إلغاء / تم التحديد / اكتب للتصفية / {n} نتائج / نتيجة واحدة / العربية / الانتقال إلى المحتوى / القائمة / الحساب / اللغة

Write each as twelve `rastrillo.ui.<key> = "<value>"` lines in en's key order. Quote values with `"`; none of the values contain a double quote or backslash.

- [ ] **Step 4: Rewrite `basecatalog.go`**

```go
package rastrillo

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
)

// baseFS carries the framework's own strings, one flat TOML catalog per
// shipped locale (design spec 2026-08-28 §3). Keys are namespaced
// rastrillo.ui.* so an app catalog can override any of them per locale
// without colliding with app keys.
//
// en.toml is the source of truth: every other file must hold exactly
// its key set (TestBaseCatalogsShareOneKeySet). ui's partials fall back
// to these keys via {{T "rastrillo.ui.*"}} — see ui/funcs.go's defaultT.
//
//go:embed locales/*.toml
var baseFS embed.FS

// baseLocales is the shipped set, en first, then spec §3.1's order:
// Ethnologue's L1 top ten, Irish, and Arabic for right-to-left coverage.
var baseLocales = []string{"en", "ga", "zh-Hans", "es", "hi", "pt", "bn", "ru", "ja", "yue", "vi", "ar"}

var baseCatalogs = func() map[string]Catalog {
	out := make(map[string]Catalog, len(baseLocales))
	for _, code := range baseLocales {
		data, err := fs.ReadFile(baseFS, "locales/"+code+".toml")
		if err != nil {
			panic(fmt.Sprintf("rastrillo: base catalog %s: %v", code, err))
		}
		m, err := catalog.Decode(data)
		if err != nil {
			panic(fmt.Sprintf("rastrillo: base catalog %s: %v", code, err))
		}
		out[code] = Catalog(m)
	}
	return out
}()

// BaseCatalog returns a copy of the framework's English strings — the
// view every existing caller (serve.go, ui's defaultT) already relies
// on. A copy, so a caller's edits cannot reach the shared table.
func BaseCatalog() Catalog { return copyCatalog(baseCatalogs["en"]) }

// BaseCatalogs returns a copy of every shipped catalog, keyed by locale
// code exactly as declared in BaseLocales.
func BaseCatalogs() map[string]Catalog {
	out := make(map[string]Catalog, len(baseCatalogs))
	for code, c := range baseCatalogs {
		out[code] = copyCatalog(c)
	}
	return out
}

// BaseLocales returns the shipped locale codes, en first.
func BaseLocales() []string { return append([]string(nil), baseLocales...) }

// BaseKeys returns the sorted rastrillo.ui.* key set — what an app
// declaring a locale the framework does not ship has to translate
// before `rastrillo generate --check` passes (spec §3.4).
func BaseKeys() []string {
	keys := make([]string, 0, len(baseCatalogs["en"]))
	for k := range baseCatalogs["en"] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// IsBaseKey reports whether key is one the framework ships.
func IsBaseKey(key string) bool {
	_, ok := baseCatalogs["en"][key]
	return ok && strings.HasPrefix(key, "rastrillo.ui.")
}

func copyCatalog(c Catalog) Catalog {
	out := make(Catalog, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}
```

Delete the old `baseCatalog` map literal. In `basecatalog_test.go`, `TestBaseCatalogIsACopy` references `baseCatalog[...]`; change both references to `baseCatalogs["en"][...]`.

- [ ] **Step 5: Run the package tests**

Run: `GOFLAGS=-mod=mod go test . ./ui/ -v -run 'BaseCatalog|Skill'`
Expected: PASS. If `TestBaseCatalogsShareOneKeySet` reports a missing key, the named TOML file is missing a line.

- [ ] **Step 6: Commit**

```bash
git add locales basecatalog.go basecatalog_test.go
git commit -m "Base catalogs: twelve shipped locales, embedded TOML, one key set

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: The framework-locale fallback level and `Dir`

**Files:**
- Modify: `locale.go`
- Modify: `locale_test.go`

**Interfaces:**
- Consumes: `BaseCatalogs()` from Task 1.
- Produces:
  - `(*Locales).T` order: app[locale] → app[default] → framework[locale] → base → key.
  - `func Dir(locale string) string` — `"rtl"` for primary subtag `ar`, `fa`, `he`, `ur`; else `"ltr"`.
  - `(*Locales).FrameworkHas(code string) bool` — whether the framework ships a catalog for a declared code.

- [ ] **Step 1: Write the failing tests**

Append to `locale_test.go`:

```go
func TestFrameworkCatalogLevel(t *testing.T) {
	// ga is shipped by the framework, fr is not; neither has an app
	// catalog for rastrillo.ui.cancel. base is what serve.go passes —
	// framework en plus the app's overlay.
	l, err := NewLocales([]string{"en", "ga", "fr"}, "en", BaseCatalog(), testFS())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, locale, key, want string
	}{
		{"shipped locale resolves the framework catalog", "ga", "rastrillo.ui.cancel", "Cealaigh"},
		{"unshipped locale falls to framework en", "fr", "rastrillo.ui.cancel", "Cancel"},
		{"app catalog still beats the framework", "fr", "app.title", "Commandes"},
		{"app default beats the framework catalog", "ga", "app.title", "Orders"},
		{"declared code must match exactly", "zh", "rastrillo.ui.cancel", "Cancel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.T(tt.locale, tt.key); got != tt.want {
				t.Errorf("T(%q,%q) = %q, want %q", tt.locale, tt.key, got, tt.want)
			}
		})
	}
	if !l.FrameworkHas("ga") || l.FrameworkHas("fr") {
		t.Error("FrameworkHas: ga yes, fr no")
	}
}

func TestAppCatalogOverridesAFrameworkKey(t *testing.T) {
	fsys := testFS()
	fsys["locales/ga.toml"] = &fstest.MapFile{Data: []byte("rastrillo.ui.cancel = \"Stad\"\n")}
	l, err := NewLocales([]string{"en", "ga"}, "en", BaseCatalog(), fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.T("ga", "rastrillo.ui.cancel"); got != "Stad" {
		t.Errorf("app ga catalog should win: %q", got)
	}
}

func TestDir(t *testing.T) {
	for in, want := range map[string]string{"ar": "rtl", "ar-EG": "rtl", "he": "rtl", "fa": "rtl", "ur": "rtl", "en": "ltr", "ga": "ltr", "": "ltr", "zh-Hans": "ltr"} {
		if got := Dir(in); got != want {
			t.Errorf("Dir(%q) = %q, want %q", in, got, want)
		}
	}
}
```

Note: the "declared code must match exactly" case declares `zh`? It does not — `zh` is undeclared here, so it resolves through the default, which has no framework Chinese. That is the behaviour spec §3.3 wants; keep the case as written.

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-mod=mod go test . -run 'TestFrameworkCatalogLevel|TestAppCatalogOverridesAFrameworkKey|TestDir' -v`
Expected: FAIL — `undefined: Dir`, `l.FrameworkHas undefined`.

- [ ] **Step 3: Implement**

In `locale.go`:

1. Add field `fw map[string]Catalog` to `Locales`.
2. In `NewLocales`, after building `l`, set `l.fw = map[string]Catalog{}` and for each declared code `c`, if `bc, ok := baseCatalogs[c]; ok { l.fw[c] = bc }`. (Use the package-level `baseCatalogs` directly — read-only.)
3. Change `T`:

```go
func (l *Locales) T(locale, key string) string {
	if v, ok := l.app[locale][key]; ok {
		return v
	}
	if v, ok := l.app[l.def][key]; ok {
		return v
	}
	if v, ok := l.fw[locale][key]; ok {
		return v
	}
	if v, ok := l.base[key]; ok {
		return v
	}
	return key
}
```

4. Add:

```go
// FrameworkHas reports whether the framework ships a base catalog for
// a declared code — matched exactly, so "zh" never finds "zh-Hans"
// (spec §3.3).
func (l *Locales) FrameworkHas(code string) bool { _, ok := l.fw[code]; return ok }

// Dir is the HTML dir attribute for a locale: "rtl" for the
// right-to-left scripts a rastrillo app can declare, "ltr" otherwise.
// Decided on the primary subtag, so ar-EG mirrors as ar does.
func Dir(locale string) string {
	switch primarySubtag(strings.ToLower(locale)) {
	case "ar", "fa", "he", "ur":
		return "rtl"
	}
	return "ltr"
}
```

5. Update the `Locales` doc comment: the lookup list gains "the framework's catalog for the requested locale, when it ships one" between the default app catalog and the base layer; delete the "English-only in v1" paragraph.

- [ ] **Step 4: Run the root tests**

Run: `GOFLAGS=-mod=mod go test . -v -run 'Locale|Dir|BaseCatalog|Lookup'`
Expected: PASS, including the pre-existing `TestLookupLayers`.

- [ ] **Step 5: Commit**

```bash
git add locale.go locale_test.go
git commit -m "Locales: framework catalog for the requested locale as a fallback level; Dir

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: `LocaleItems` and the switch route

**Files:**
- Modify: `localemw.go`
- Modify: `localemw_test.go`
- Modify: `serve.go` (`buildHandler`)
- Modify: `serve_router_test.go`

**Interfaces:**
- Consumes: `(*Locales).T`, `Has`, `Codes`, `splitPrefix`, `LocaleCookie`.
- Produces:
  - `type LocaleItem struct { Code, Name, Href string; Current bool }`
  - `func LocaleItems(r *http.Request) []LocaleItem` — empty outside the middleware or with one declared locale.
  - `const LocaleSwitchPath = "/_locale"`
  - `func (l *Locales) SwitchHandler() http.Handler` — `POST` only; form fields `locale`, `return`.

- [ ] **Step 1: Write the failing tests**

Append to `localemw_test.go`:

```go
func TestLocaleItems(t *testing.T) {
	l := mwLocales(t) // en, fr, de-informal; default en
	var items []LocaleItem
	h := l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		items = LocaleItems(r)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fr/orders?page=2", nil))
	want := []LocaleItem{
		{Code: "en", Name: "English", Href: "/en/orders?page=2"},
		{Code: "fr", Name: "fr", Href: "/fr/orders?page=2", Current: true},
		{Code: "de-informal", Name: "de-informal", Href: "/de-informal/orders?page=2"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("items = %+v\nwant  %+v", items, want)
	}
}

func TestLocaleItemsEmptyForOneLocaleOrNoMiddleware(t *testing.T) {
	if got := LocaleItems(httptest.NewRequest("GET", "/", nil)); len(got) != 0 {
		t.Errorf("without middleware: %v", got)
	}
	l, _ := NewLocales([]string{"en"}, "en", BaseCatalog(), nil)
	var got []LocaleItem
	l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = LocaleItems(r) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if len(got) != 0 {
		t.Errorf("one locale: %v", got)
	}
}

func switchReq(locale, ret string) *http.Request {
	form := url.Values{"locale": {locale}, "return": {ret}}
	req := httptest.NewRequest("POST", LocaleSwitchPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return req
}

func TestSwitchHandlerSetsCookieAndRedirects(t *testing.T) {
	l := mwLocales(t)
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, switchReq("fr", "/orders?page=2"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/fr/orders?page=2" {
		t.Errorf("Location = %q", got)
	}
	var c *http.Cookie
	for _, k := range rec.Result().Cookies() {
		if k.Name == LocaleCookie {
			c = k
		}
	}
	if c == nil || c.Value != "fr" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge < 86400*300 {
		t.Errorf("cookie = %+v", c)
	}
}

func TestSwitchHandlerStripsAnExistingPrefixFromReturn(t *testing.T) {
	l := mwLocales(t)
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, switchReq("en", "/fr/orders"))
	if got := rec.Header().Get("Location"); got != "/en/orders" {
		t.Errorf("Location = %q, want /en/orders", got)
	}
}

func TestSwitchHandlerRefusals(t *testing.T) {
	l := mwLocales(t)
	tests := []struct {
		name   string
		req    *http.Request
		status int
	}{
		{"undeclared locale", switchReq("es", "/"), http.StatusBadRequest},
		{"protocol-relative return", switchReq("fr", "//evil.example/"), http.StatusBadRequest},
		{"absolute return", switchReq("fr", "https://evil.example/"), http.StatusBadRequest},
		{"GET", httptest.NewRequest("GET", LocaleSwitchPath, nil), http.StatusMethodNotAllowed},
	}
	crossSite := switchReq("fr", "/")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	tests = append(tests, struct {
		name   string
		req    *http.Request
		status int
	}{"cross-site", crossSite, http.StatusForbidden})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			l.SwitchHandler().ServeHTTP(rec, tt.req)
			if rec.Code != tt.status {
				t.Errorf("status %d, want %d", rec.Code, tt.status)
			}
			if rec.Header().Get("Set-Cookie") != "" {
				t.Error("a refusal must not set the cookie")
			}
		})
	}
}

func TestSwitchHandlerEmptyReturnGoesHome(t *testing.T) {
	l := mwLocales(t)
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, switchReq("fr", ""))
	if got := rec.Header().Get("Location"); got != "/fr/" {
		t.Errorf("Location = %q, want /fr/", got)
	}
}

func TestSwitchHandlerSecureCookieBehindTLS(t *testing.T) {
	l := mwLocales(t)
	req := switchReq("fr", "/")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == LocaleCookie && !c.Secure {
			t.Error("cookie must be Secure when the request arrived over https")
		}
	}
}
```

Add `"net/url"` and `"reflect"` to the imports.

Append to `serve_router_test.go` (it already has a `buildHandler` harness; follow its existing style for constructing `Options` with a `Mux`):

```go
func TestLocaleSwitchRouteMountedOnlyWithLocales(t *testing.T) {
	mux := http.NewServeMux()
	without, err := buildHandler(Options{Mux: mux})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	without.ServeHTTP(rec, httptest.NewRequest("POST", LocaleSwitchPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no locales: status %d, want 404", rec.Code)
	}

	with, err := buildHandler(Options{Mux: http.NewServeMux(), Locales: []string{"en", "ga"}})
	if err != nil {
		t.Fatal(err)
	}
	form := strings.NewReader("locale=ga&return=/x")
	req := httptest.NewRequest("POST", LocaleSwitchPath, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	with.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ga/x" {
		t.Errorf("with locales: status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	// Under a locale prefix the same route still answers (the
	// middleware strips the prefix first).
	req = httptest.NewRequest("POST", "/ga"+LocaleSwitchPath, strings.NewReader("locale=en&return=/x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	with.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("prefixed: status %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-mod=mod go test . -run 'LocaleItems|SwitchHandler|LocaleSwitchRoute' -v`
Expected: compile errors for `LocaleItem`, `LocaleSwitchPath`, `SwitchHandler`.

- [ ] **Step 3: Implement in `localemw.go`**

```go
// LocaleSwitchPath is the framework route the language switcher POSTs
// to (spec §2.4). Mounted by Serve whenever Options.Locales is set.
const LocaleSwitchPath = "/_locale"

// LocaleItem is one entry of the language switcher: the declared code,
// its autonym (rastrillo.ui.locale_name in that locale, or the code
// when no catalog names it), a plain link to the same path under that
// locale's prefix, and whether it is the request's locale.
type LocaleItem struct {
	Code    string
	Name    string
	Href    string
	Current bool
}

// LocaleItems builds the switcher's data for r. Empty when the request
// never went through Middleware or the app declares one locale — the
// partial renders nothing for an empty list, so a one-locale app can
// call it unconditionally.
func LocaleItems(r *http.Request) []LocaleItem {
	l, ok := r.Context().Value(localesCtxKey{}).(*Locales)
	if !ok || len(l.codes) < 2 {
		return nil
	}
	cur := LocaleFrom(r)
	rest := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		rest += "?" + r.URL.RawQuery
	}
	items := make([]LocaleItem, 0, len(l.codes))
	for _, c := range l.codes {
		items = append(items, LocaleItem{
			Code:    c,
			Name:    l.T(c, "rastrillo.ui.locale_name"),
			Href:    "/" + c + rest,
			Current: c == cur,
		})
	}
	return items
}

// SwitchHandler answers POST /_locale: it stores the chosen locale in
// LocaleCookie and 303s to the return path under that locale's prefix.
// Same-origin is checked the way every mutating route in this
// framework checks it (csrf.SameOrigin), with the origin taken from the
// request itself — the handler has no configured origin and needs
// none, because the check is "did a page of ours submit this".
func (l *Locales) SwitchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		if !csrf.SameOrigin(r, scheme+"://"+r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		code := r.PostFormValue("locale")
		if !l.Has(code) {
			http.Error(w, "unknown locale", http.StatusBadRequest)
			return
		}
		ret := r.PostFormValue("return")
		if ret == "" {
			ret = "/"
		}
		if !strings.HasPrefix(ret, "/") || strings.HasPrefix(ret, "//") || strings.HasPrefix(ret, "/\\") {
			http.Error(w, "bad return path", http.StatusBadRequest)
			return
		}
		// A return path that already carries a locale prefix loses it,
		// so switching from /fr/orders lands on /en/orders, not
		// /en/fr/orders.
		if _, rest := l.splitPrefix(strings.SplitN(ret, "?", 2)[0]); rest != "" {
			if i := strings.Index(ret, "?"); i >= 0 {
				rest += ret[i:]
			}
			ret = rest
		}
		http.SetCookie(w, &http.Cookie{
			Name: LocaleCookie, Value: code, Path: "/",
			MaxAge: 365 * 24 * 3600, HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Secure: scheme == "https",
		})
		http.Redirect(w, r, "/"+code+ret, http.StatusSeeOther)
	})
}
```

Add `"amadan.net/rastrillo/rastrillo/csrf"` to the imports. Check `csrf` does not import the root package (`grep -n rastrillo csrf/csrf.go` must show only the package's own path); it does not today.

In `serve.go`'s `buildHandler`, after `loc, err := NewLocales(...)` succeeds and before the return:

```go
	mux.Handle("POST "+LocaleSwitchPath, loc.SwitchHandler())
```

This must come after `mux.Handle("/", app)`; a more specific pattern wins in `ServeMux` regardless of registration order, so placement only matters for readability.

- [ ] **Step 4: Run**

Run: `GOFLAGS=-mod=mod go test . -v -run 'LocaleItems|SwitchHandler|LocaleSwitchRoute'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add localemw.go localemw_test.go serve.go serve_router_test.go
git commit -m "Locale switcher: LocaleItems and POST /_locale, which writes the cookie

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Cookie precedence above Accept-Language

**Files:**
- Modify: `localemw.go` (`Middleware`)
- Modify: `localemw_test.go` (`TestResolutionPrecedence`)

**Interfaces:**
- Produces: resolution order prefix → cookie (declared) → `Accept-Language` → default.

- [ ] **Step 1: Change the test table**

In `TestResolutionPrecedence`, replace the two cookie rows:

```go
		{"cookie beats Accept-Language", "/orders", "fr", "de-informal", "de-informal", "/orders"},
		{"cookie only when it names a declared locale", "/orders", "fr", "es", "fr", "/orders"},
```

and add:

```go
		{"prefix still beats the cookie", "/fr/orders", "", "de-informal", "fr", "/orders"},
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-mod=mod go test . -run TestResolutionPrecedence -v`
Expected: FAIL on "cookie beats Accept-Language" (got `fr`).

- [ ] **Step 3: Reorder in `Middleware`**

```go
		code, rest := l.splitPrefix(r.URL.Path)
		if code == "" {
			if c, err := r.Cookie(LocaleCookie); err == nil && l.Has(c.Value) {
				code = c.Value
			}
		}
		if code == "" {
			code = l.negotiate(r.Header.Get("Accept-Language"))
		}
		if code == "" {
			code = l.def
		}
```

Rewrite the `Middleware` doc comment's precedence paragraph:

```go
// Precedence: URL path prefix, then the stored-preference cookie, then
// Accept-Language, then the default. The original design doc (§10) put
// the cookie last; that was reversed on 2026-08-28 when the framework
// started writing the cookie itself (SwitchHandler) — a stored choice
// that Accept-Language could override on the next request would make
// the switcher decorative.
```

- [ ] **Step 4: Run the whole root package**

Run: `GOFLAGS=-mod=mod go test .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add localemw.go localemw_test.go
git commit -m "Locale resolution: a stored choice beats Accept-Language

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: The `locale-menu` partial and its CSS

**Files:**
- Create: `ui/partials/locale-menu.html`
- Modify: `ui/tokens.css` (append near the dropdown idiom, before the form family)
- Copy: `ui/tokens.css` → `examples/blog/static/tokens.css`, `examples/tickets/static/tokens.css`, `examples/notes/static/tokens.css` (if the notes example has one — check with `ls examples/notes/static`)
- Modify: `ui/ui_test.go`

**Interfaces:**
- Consumes: `[]rastrillo.LocaleItem` (Task 3), `T` func, `rastrillo.LocaleSwitchPath`.
- Produces: partial `locale-menu` with keys `Items []LocaleItem` (required), `Action string` (optional, default `/_locale`), `Return string` (optional — the current path+query; when empty the form omits the field and the handler goes home).

- [ ] **Step 1: Add to the defined-partials list and write the render test**

In `ui/ui_test.go`, `TestAllPartialsAreDefined`: add `"locale-menu"` to the `want` slice and change the count message and comparison from 28 to 29.

Append:

```go
func TestLocaleMenuRenders(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
	items := []rastrillo.LocaleItem{
		{Code: "en", Name: "English", Href: "/en/orders"},
		{Code: "ga", Name: "Gaeilge", Href: "/ga/orders", Current: true},
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "locale-menu", map[string]any{"Items": items, "Return": "/orders?page=2"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`<details class="rst-dropdown rst-locale"`,
		`action="/_locale"`,
		`name="locale" value="ga"`,
		`name="return" value="/orders?page=2"`,
		`aria-current="true"`,
		`lang="ga"`,
		`>Gaeilge<`,
		`Language`, // the summary, from the base catalog via defaultT
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	b.Reset()
	if err := tmpl.ExecuteTemplate(&b, "locale-menu", map[string]any{"Items": []rastrillo.LocaleItem{}}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != "" {
		t.Errorf("empty Items must render nothing, got %q", b.String())
	}
}
```

`ui_test.go` already imports the root package as `rastrillo` for other tests; if not, add `"amadan.net/rastrillo/rastrillo"`.

- [ ] **Step 2: Run to verify failure**

Run: `cd ui && GOFLAGS=-mod=mod go test . -run 'TestAllPartialsAreDefined|TestLocaleMenuRenders' -v`
Expected: FAIL — partial not defined.

- [ ] **Step 3: Write the partial**

`ui/partials/locale-menu.html`:

```html
{{/* locale-menu — the language switcher (design spec 2026-08-28 §2.4):
     a details/summary dropdown listing the app's declared locales,
     each item a one-field POST form to /_locale so the choice is stored
     in the locale cookie and the user lands on the same path under the
     new prefix. No JavaScript. Renders nothing for an empty Items, so
     a one-locale app calls it unconditionally.

     Each item's autonym is marked with its own lang so a screen reader
     switches voice to say "Gaeilge" in Irish. The current locale is
     aria-current and rendered as a button too, so the list is uniform
     for keyboard users; submitting it is a harmless no-op.

     Keys:
       Items   []rastrillo.LocaleItem, required — from rastrillo.LocaleItems(r)
       Return  string, optional — current path+query to come back to;
               omit and the switch lands on the locale's home
       Action  string, optional — default /_locale (rastrillo.LocaleSwitchPath) */}}
{{define "locale-menu"}}{{if .Items}}<details class="rst-dropdown rst-locale">
  <summary>{{T "rastrillo.ui.shell_language"}}<span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span></summary>
  <div class="rst-dropdown__menu">
  {{- $action := or .Action "/_locale"}}{{$return := .Return}}
  {{- range .Items}}
    <form method="post" action="{{$action}}"><input type="hidden" name="locale" value="{{.Code}}">{{if $return}}<input type="hidden" name="return" value="{{$return}}">{{end}}<button type="submit" lang="{{.Code}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</button></form>
  {{- end}}
  </div>
</details>{{end}}{{end}}
```

- [ ] **Step 4: Add the CSS**

In `ui/tokens.css`, directly after the `.rst-dropdown__menu a` rules (find with `grep -n "rst-dropdown__menu" ui/tokens.css`), add:

```css
/* locale-menu — the language switcher is a dropdown whose items are
   one-field POST forms (spec §2.4), so the button inside each form
   takes the menu-link styling. Autonyms keep their own script, so no
   text-transform here, ever. */
.rst-locale form { margin: 0; }
.rst-locale button { background: none; border: 0; border-radius: var(--rst-radius-sm); color: var(--rst-text); cursor: pointer; display: block; font: inherit; font-size: var(--rst-fs-sm); padding: 0.4rem 0.65rem; text-align: start; width: 100%; }
.rst-locale button:hover { background: var(--rst-accent-soft); }
.rst-locale button[aria-current] { color: var(--rst-accent); font-weight: 600; }
```

Then copy the file to every example that vendors it:

```bash
for d in examples/*/static; do [ -f "$d/tokens.css" ] && cp ui/tokens.css "$d/tokens.css"; done
```

- [ ] **Step 5: Run the ui tests and the examples' pins**

Run: `cd ui && GOFLAGS=-mod=mod go test . -v -run 'Partials|LocaleMenu|IdiomClasses|Styled|SelfContained'` then `for d in examples/blog examples/tickets examples/notes; do (cd $d && GOFLAGS=-mod=mod go test ./... 2>&1 | grep -v '^ok\|no test files'); done`
Expected: all PASS; no output from the examples loop other than nothing (a failing pin prints `static/tokens.css differs`).

- [ ] **Step 6: Commit**

```bash
git add ui/partials/locale-menu.html ui/tokens.css ui/ui_test.go examples/*/static/tokens.css
git commit -m "ui: locale-menu partial, the no-JS language switcher

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: The `--check` rule for framework keys

**Files:**
- Modify: `internal/generate/locales.go`
- Modify: `internal/generate/locales_test.go`
- Modify: `cmd/rastrillo/generate.go`

**Interfaces:**
- Consumes: `rastrillo.BaseKeys()`, `rastrillo.BaseLocales()` from Task 1 (passed in by `cmd`, so `internal/generate` does not import the root package).
- Produces: `func MissingFrameworkKeys(localesDir, defaultCode string, frameworkKeys, shipped []string) (map[string][]string, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/generate/locales_test.go` (the file has a helper that writes a temp `locales/` dir — reuse it; if it is named differently, adapt the two calls below):

```go
func TestMissingFrameworkKeys(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("en.toml", "app.title = \"Orders\"\n")
	write("fr.toml", "app.title = \"Commandes\"\nrastrillo.ui.cancel = \"Annuler\"\n")
	write("ga.toml", "app.title = \"Orduithe\"\n") // shipped by the framework: exempt
	keys := []string{"rastrillo.ui.cancel", "rastrillo.ui.done"}
	got, err := MissingFrameworkKeys(dir, "en", keys, []string{"en", "ga"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"fr": {"rastrillo.ui.done"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMissingFrameworkKeysNoLocalesDir(t *testing.T) {
	got, err := MissingFrameworkKeys(filepath.Join(t.TempDir(), "nope"), "en", []string{"k"}, nil)
	if err != nil || got != nil {
		t.Errorf("got %v, %v", got, err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-mod=mod go test ./internal/generate -run MissingFrameworkKeys -v`
Expected: `undefined: MissingFrameworkKeys`.

- [ ] **Step 3: Implement**

Refactor `MissingKeys` so the directory read is shared. In `internal/generate/locales.go`:

```go
// readCatalogs decodes every <code>.toml in localesDir. nil, nil when
// the directory does not exist — the single-locale case.
func readCatalogs(localesDir string) (map[string]map[string]string, error) {
	entries, err := os.ReadDir(localesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	catalogs := map[string]map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(localesDir, name))
		if err != nil {
			return nil, err
		}
		m, err := catalog.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		catalogs[strings.TrimSuffix(name, ".toml")] = m
	}
	return catalogs, nil
}

// MissingFrameworkKeys is spec 2026-08-28 §3.4's rule: a non-default
// catalog for a locale the framework does NOT ship must translate every
// rastrillo.ui.* key, or its built-in components render in English and
// nobody is told. A locale in shipped is exempt — the framework's own
// catalog covers it. frameworkKeys and shipped come from the caller
// (rastrillo.BaseKeys, rastrillo.BaseLocales) so this package does not
// import the root.
func MissingFrameworkKeys(localesDir, defaultCode string, frameworkKeys, shipped []string) (map[string][]string, error) {
	catalogs, err := readCatalogs(localesDir)
	if err != nil || len(catalogs) == 0 {
		return nil, err
	}
	exempt := map[string]bool{}
	for _, s := range shipped {
		exempt[s] = true
	}
	out := map[string][]string{}
	for code, c := range catalogs {
		if code == defaultCode || exempt[code] {
			continue
		}
		var missing []string
		for _, k := range frameworkKeys {
			if _, ok := c[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			out[code] = missing
		}
	}
	return out, nil
}
```

Replace the body of `MissingKeys` up to `if len(catalogs) == 0` with `catalogs, err := readCatalogs(localesDir); if err != nil || len(catalogs) == 0 { return nil, err }`. Keep everything after unchanged.

- [ ] **Step 4: Wire the CLI**

In `cmd/rastrillo/generate.go`, immediately after the existing `MissingKeys` block (after its `return fmt.Errorf(...)`), add:

```go
		fw, err := generate.MissingFrameworkKeys(filepath.Join(dir, "locales"), *defaultLocale, rastrillo.BaseKeys(), rastrillo.BaseLocales())
		if err != nil {
			return fmt.Errorf("i18n framework-key check: %w", err)
		}
		if len(fw) > 0 {
			codes := make([]string, 0, len(fw))
			for code := range fw {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			fmt.Fprintln(os.Stderr, "rastrillo generate: built-in components would render in English —")
			for _, code := range codes {
				fmt.Fprintf(os.Stderr, "  locales/%s.toml is missing %d rastrillo.ui.* key(s):\n", code, len(fw[code]))
				for _, key := range fw[code] {
					fmt.Fprintf(os.Stderr, "    %s\n", key)
				}
			}
			fmt.Fprintf(os.Stderr, "the framework ships %s; any other locale translates these itself — copy the keys from the rastrillo module's locales/en.toml\n", strings.Join(rastrillo.BaseLocales(), ", "))
			return fmt.Errorf("%d locale catalog(s) leave built-in strings untranslated", len(fw))
		}
```

Ensure `"amadan.net/rastrillo/rastrillo"` and `"strings"` are imported in that file (check existing imports first).

- [ ] **Step 5: Run**

Run: `GOFLAGS=-mod=mod go test ./internal/generate ./cmd/... `
Expected: PASS. Then a manual check: `cd examples/blog && go run ../../cmd/rastrillo generate --check` must still exit 0 (blog ships only `en`).

- [ ] **Step 6: Commit**

```bash
git add internal/generate/locales.go internal/generate/locales_test.go cmd/rastrillo/generate.go
git commit -m "generate --check: an unshipped locale must translate the framework's keys

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Docs and SKILL.md

**Files:**
- Modify: `docs/site/localization.md`
- Modify: `docs/site/templates.md`
- Modify: `SKILL.md`

- [ ] **Step 1: `localization.md`**

Replace the "How a locale gets picked" section body with:

```markdown
In this order: a URL path prefix, stripped before your mux sees it so
`/fr/orders` and `/orders` reach the same route; then the
`rastrillo_locale` cookie, when it names a declared locale; then
`Accept-Language`, q-ordered, so a browser sending `fr-CA` matches a
declared `fr`; then your default.

The cookie is written by the framework's own `POST /_locale` route,
which `rastrillo.Serve` mounts whenever you declare more than one
locale. The `locale-menu` partial renders a switcher that posts to it:

```html
{{template "locale-menu" dict "Items" .Locales "Return" .Path}}
```

where `.Locales` is `rastrillo.LocaleItems(r)` — empty for a one-locale
app, so the partial renders nothing — and `.Path` is the current path
and query to return to. Each item lands on the same path under the new
prefix, and the cookie makes the choice stick on unprefixed paths too.
```

Replace "The framework base catalog" section body with:

```markdown
The framework ships its own strings — `rastrillo/ui`'s `rastrillo.ui.*`
keys — in twelve locales: `en`, `ga`, `zh-Hans`, `es`, `hi`, `pt`, `bn`,
`ru`, `ja`, `yue`, `vi`, `ar`. Declare one of those and the built-in
components speak it with no catalog of your own. Matching is by the code
you declare, exactly: `zh` does not find `zh-Hans`.

Lookup falls back through five levels: the requested locale's app
catalog, the default locale's app catalog, the framework's catalog for
the requested locale (when it ships one), the framework's English, and
finally the key itself. A missing translation stays visible on the page
as `orders.title` — never blank, never a crash.

Your own catalog entry for the same key wins, so you can reword a
component's built-in string without ejecting it.

`rastrillo.Dir(locale)` gives the `<html dir>` value — `rtl` for
Arabic, Persian, Hebrew and Urdu — so a layout never guesses.
```

Keep the `ui.FuncsWith` paragraph and example as they are.

In "The pre-ship gate", after the sentence "Fails when a non-default catalog is missing keys the default has.", add:

```markdown
It also fails when a non-default catalog is for a locale the framework
does not ship and leaves any `rastrillo.ui.*` key untranslated — those
components would silently render in English. The message lists the keys;
copy them from the module's `locales/en.toml`.
```

- [ ] **Step 2: `templates.md`**

In the partials list code block, add `locale-menu` after `list-row-action` keeping the columns roughly aligned. After the block, add one sentence: "`locale-menu` is the language switcher; see [Localization](/docs/localization)."

- [ ] **Step 3: `SKILL.md`**

In §5 (Sessions and identity) or wherever locales are mentioned — there is currently no locale line, so append to the end of §1 App shape, before §2's heading:

```markdown
Locales: `Options.Locales`/`DefaultLocale`/`LocaleFS` (flat TOML per code).
The framework ships `rastrillo.ui.*` in en ga zh-Hans es hi pt bn ru ja yue
vi ar; any other declared locale must translate them or `generate --check`
fails. `{{template "locale-menu" dict "Items" (rastrillo.LocaleItems r)}}`
is the switcher; it POSTs `/_locale`, which Serve mounts. `rastrillo.Dir`
for `<html dir>`.
```

Then run `wc -c SKILL.md`; if over 18,000, trim §7's manifest bullet (its "Full treatment" line can go) until under.

- [ ] **Step 4: Run every gate**

Run: `GOFLAGS=-mod=mod go test . ./ui/ ./internal/... ./cmd/...` and the examples loop from Task 5 Step 5.
Expected: PASS everywhere, including `internal/docsite` (links in the new prose resolve: `/docs/localization` exists).

- [ ] **Step 5: Commit**

```bash
git add docs/site/localization.md docs/site/templates.md SKILL.md
git commit -m "Docs: twelve base locales, the switcher, cookie precedence

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: Open the PR

- [ ] **Step 1: Push and open**

```bash
git push -u origin HEAD
gh pr create --title "Base languages: twelve framework catalogs, a locale switcher, and the --check rule" --body "$(cat <<'EOF'
PR 1 of 5 for the design-system spec (docs/superpowers/specs/2026-08-28-design-system-design.md, §0, §2.4, §3).

- `locales/*.toml` × 12 embedded in the root package; `BaseCatalogs()`, `BaseLocales()`, `BaseKeys()`; `BaseCatalog()` unchanged as the en view. Gate: every catalog holds exactly en's key set.
- `(*Locales).T` gains a level: framework catalog for the requested locale, matched by declared code exactly.
- `LocaleItems(r)` + `locale-menu` partial + `POST /_locale` (mounted by Serve with ≥1 locale) writing `rastrillo_locale`. Same-origin checked via `csrf.SameOrigin`.
- **Behaviour change:** cookie now beats `Accept-Language` (prefix still wins). Without it the switcher would be undone on the next unprefixed request.
- `rastrillo generate --check` fails an unshipped locale that leaves `rastrillo.ui.*` untranslated.
- `rastrillo.Dir(locale)` for `<html dir>`.
- Catalogs are machine-drafted and say so in their headers.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 2: Watch CI**

Run: `gh pr checks --watch`
Expected: `test` and `browser` pass.

---

## Self-review

**Spec coverage.** §3.1 set → Task 1. §3.2 key set + gate → Task 1. §3.3 fallback level and exact matching → Task 2. §3.4 `--check` → Task 6. §2.4 switcher, cookie route, `LocaleItems`, autonyms, `aria-current`, hidden for one locale → Tasks 3, 5. `Dir` → Task 2 (in root, deviation recorded). Localization doc's "nothing writes that cookie" removed → Task 7. §0 no-hardcoded-strings: the partial's only literal is the summary, via `T`. Not in this PR by design: `shell_*` keys are defined but unused until PR 2; `date_*` and `field_required` arrive with PR 3.

**Placeholders.** None; every step has code or exact prose.

**Type consistency.** `LocaleItem{Code, Name, Href, Current}` used identically in Tasks 3, 5. `MissingFrameworkKeys(localesDir, defaultCode, frameworkKeys, shipped)` matches between Task 6 test, implementation and CLI. `LocaleSwitchPath` used in Tasks 3, 5 (`/_locale` literal in the partial's default matches the constant). `FrameworkHas` is defined in Task 2 and used only in its test — retained because PR 2's shells need it for the `lang` fallback.
