// Package generate's money action helpers (this file): what a
// generated action emits for a money field, from display to clear.
//
// A money field is two columns read as pointers (store.go), so an
// empty field is nil in both, and every amount has its currency. The
// actions keep that rule on every path:
//
//   - display formats in Go, in the reader's formatting locale, and a
//     stored code Format refuses answers 500 naming the row, the field
//     and the code (op.generate.format_failed), never a wrong amount;
//   - a form field carries the currency the server will store in: the
//     resource default on create, the record's own on edit (the default
//     only while the field is empty), so an update never writes a
//     currency different from a set one;
//   - a blank field writes NULL to both columns, which is how an
//     optional amount is cleared;
//   - an edit form's input is seeded from the record, and the POST
//     passes the record's amount as Stored, so the unchanged-seed check
//     compares with what the record holds now.
package generate

import (
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// formatFailed is Appendix C.2's op.generate.format_failed: the
// resource, the row id, the field and the code a stored amount could
// not be shown in.
const formatFailed = "%s %d: %s in %s"

// lowerFirst is a declared field name as a local's prefix: "Price" ->
// "price". Locals add a suffix (Text, Input, Code, Minor, Currency), so
// a field named Store or Page cannot shadow a local of the same name.
func lowerFirst(s string) string {
	return strings.ToLower(s[:1]) + s[1:]
}

// moneyFields is fields' money fields.
func moneyFields(fields []rastrillo.Field) []rastrillo.Field {
	var out []rastrillo.Field
	for _, f := range fields {
		if f.Kind == rastrillo.Money {
			out = append(out, f)
		}
	}
	return out
}

// formFields is r's form fields, Basics then Advanced: the fields a
// create form posts and an edit form shows across its two groups.
func formFields(r rastrillo.Resource) []rastrillo.Field {
	return append(append([]rastrillo.Field(nil), r.Form.Basics...), r.Form.Advanced...)
}

// displayHelper is the money function an action that shows amounts
// emits once: "" for an empty field, else the amount in the reader's
// formatting locale. It answers the 500 itself, so a call site is two
// lines whichever page it is on. A code Format refuses was not written
// by the app's actions, so the request fails naming the row, the field
// and the code rather than show an amount in the wrong currency or
// none. The emitted helpers carry no comments: a comment in a file
// written into an app is copy, and these have none approved.
func displayHelper(r rastrillo.Resource) string {
	return fmt.Sprintf(`
func money(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, id int64, field string, minor *int64, cur *string) (string, bool) {
	if minor == nil {
		return "", true
	}
	var code string
	if cur != nil {
		code = *cur
	}
	s, err := currency.Format(rastrillo.FormatLocale(r), *minor, code)
	if err != nil {
		view.Fail(ctx, w, r, fmt.Sprintf(%q, %q, id, field, code), err)
		return "", false
	}
	return s, true
}
`, formatFailed, r.Name)
}

// seedHelper is the function a form action emits once to draw a money
// input from a stored amount (nil: none) in the reader's locale. Its
// text is also the seed (generated fields are never Strict), so an
// unchanged edit saves what the record holds even when the text reads
// two ways (en "1.500" KWD).
func seedHelper(r rastrillo.Resource) string {
	return fmt.Sprintf(`
func seed(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, id int64, field string, minor *int64, code string) (form.MoneyInput, bool) {
	in, err := form.SeedMoney(rastrillo.FormatLocale(r), minor, code, false)
	if err != nil {
		view.Fail(ctx, w, r, fmt.Sprintf(%q, %q, id, field, code), err)
		return in, false
	}
	return in, true
}
`, formatFailed, r.Name)
}

// codeHelper is the function an edit action emits once for the
// currency a field is read and stored in: the record's, or the
// resource default while the field is empty. An update therefore never
// writes a currency other than the one already set; changing it is an
// operation of its own (spec §7).
func codeHelper(r rastrillo.Resource) string {
	return fmt.Sprintf(`
func currencyOf(cur *string) string {
	if cur != nil {
		return *cur
	}
	return %q
}
`, r.Currency)
}

// amountHelper is the function a POST action emits once for what a
// money field writes: NULL in both columns for a blank field, which is
// how an optional amount is cleared, else the amount with the server's
// currency, never the form's.
const amountHelper = `
func amount(p *form.Parsed, name, code string) (*int64, *string) {
	minor, ok := p.Minor(name)
	if !ok {
		return nil, nil
	}
	return &minor, &code
}
`

// displayStmts formats c's amount off record v into <field>Text, and
// returns the expression that reads it; a non-money column is read as
// it is. idExpr names the row in a failure.
func displayStmts(b *strings.Builder, v string, c column, idExpr string) string {
	if c.Kind != rastrillo.Money {
		return v + "." + c.Name
	}
	text := lowerFirst(c.Name) + "Text"
	fmt.Fprintf(b, "%s, ok := money(ctx, w, r, %s, %q, %s.%sMinor, %s.%sCurrency)\nif !ok {\n\treturn\n}\n",
		text, idExpr, c.Name, v, c.Name, v, c.Name)
	return text
}

// moneyField is f's form.Field for a create (code a literal, the
// resource default) or an edit (code the local currencyOf set, and
// Stored the record's amount, re-read by this request).
func moneyField(f rastrillo.Field, edit bool, r rastrillo.Resource) string {
	parts := []string{fmt.Sprintf("Name: %q", f.Name), "Kind: form.Money"}
	if edit {
		parts = append(parts, "Currency: "+lowerFirst(f.Name)+"Code")
	} else {
		parts = append(parts, fmt.Sprintf("Currency: %q", r.Currency))
	}
	parts = append(parts, "LocaleEcho: true")
	if f.Required {
		parts = append(parts, "Required: true")
	}
	if edit {
		parts = append(parts, "Stored: n."+f.Name+"Minor")
	}
	return "form.Field{" + strings.Join(parts, ", ") + "}"
}

// codeStmts sets <field>Code for each money field in fields, before an
// edit POST parses them.
func codeStmts(fields []rastrillo.Field) string {
	var b strings.Builder
	for _, f := range moneyFields(fields) {
		fmt.Fprintf(&b, "%sCode := currencyOf(n.%sCurrency)\n", lowerFirst(f.Name), f.Name)
	}
	return b.String()
}

// writeStmts sets <field>Minor and <field>Currency for each money field
// in fields, from the parsed form. code is the currency expression for
// a field: the resource default on create, <field>Code on edit.
func writeStmts(fields []rastrillo.Field, code func(rastrillo.Field) string) string {
	var b strings.Builder
	for _, f := range moneyFields(fields) {
		l := lowerFirst(f.Name)
		fmt.Fprintf(&b, "%sMinor, %sCurrency := amount(p, %q, %s)\n", l, l, f.Name, code(f))
	}
	return b.String()
}

// moneyMapLiteral is formView's Money, from each money field's
// <field>Input local.
func moneyMapLiteral(fields []rastrillo.Field) string {
	var b strings.Builder
	b.WriteString("map[string]form.MoneyInput{\n")
	for _, f := range moneyFields(fields) {
		fmt.Fprintf(&b, "%q: %sInput,\n", f.Name, lowerFirst(f.Name))
	}
	b.WriteString("}")
	return b.String()
}

// seedStmts sets <field>Input for each money field in fields from the
// stored amount (minor, the record's or nil) in code's currency.
func seedStmts(fields []rastrillo.Field, idExpr string, minor, code func(rastrillo.Field) string) string {
	var b strings.Builder
	for _, f := range moneyFields(fields) {
		fmt.Fprintf(&b, "%sInput, ok := seed(ctx, w, r, %s, %q, %s, %s)\nif !ok {\n\treturn\n}\n",
			lowerFirst(f.Name), idExpr, f.Name, minor(f), code(f))
	}
	return b.String()
}

// echoStmts sets <field>Input for each money field in fields from what
// this POST submitted (the person's own text, in the locale it was
// read in). When its locale or seed was refused the text's meaning is
// unknown, so the input is seeded from the server instead: blank on
// create, which is what MoneyInput already returns, and the record on
// edit.
func echoStmts(fields []rastrillo.Field, edit bool) string {
	var b strings.Builder
	for _, f := range moneyFields(fields) {
		l := lowerFirst(f.Name)
		if !edit {
			fmt.Fprintf(&b, "%sInput, _ := p.MoneyInput(%q, rastrillo.FormatLocale(r))\n", l, f.Name)
			continue
		}
		fmt.Fprintf(&b, "%sInput, reseed := p.MoneyInput(%q, rastrillo.FormatLocale(r))\n", l, f.Name)
		fmt.Fprintf(&b, "if reseed {\nif %sInput, ok = seed(ctx, w, r, id, %q, n.%sMinor, %sCode); !ok {\nreturn\n}\n}\n", l, f.Name, f.Name, l)
	}
	return b.String()
}
