package opsgen

import (
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
)

// typesGo renders types.go: the result type, the inputs, the argument
// types Invoke decodes, the operations, the pure validators, the row
// mapping and the descriptors with their schemas. Validation lives
// here rather than in create.go and update.go because a hand-written
// create.go replaces only Create, and the page door keeps calling
// ValidateCreate from the file that stays.
func typesGo(r resource) string {
	money := r.inputMoney()
	var b strings.Builder
	b.WriteString(comment(fmt.Sprintf("Package %s holds the %s resource's operations: one function per action, called by the page door and the API door alike, so the rules are written once. A hand-written list.go, get.go, create.go, update.go or delete.go under ops/%s/ replaces the generated file of that name; this file is the contract and stays.", r.Pkg, r.Name, r.Name)))
	fmt.Fprintf(&b, "package %s\n\n", r.Pkg)

	std := []string{"context", "database/sql", "encoding/json", "errors"}
	mod := []string{}
	if money {
		std = append(std, "fmt")
		mod = append(mod, "currency")
	}
	if r.requiredText() {
		std = append(std, "strings")
	}
	mod = append(mod, "op")
	b.WriteString(imports(std, mod, &r))

	// The result type.
	b.WriteString(comment(fmt.Sprintf("%s is one %s as both doors answer it. It is the API's result type and never a template's view model, so a field the manifest declares cannot go missing from one door only.", r.Singular, r.HumanOne)))
	fmt.Fprintf(&b, "type %s struct {\nID int64 `json:\"id\"`\n", r.Singular)
	for _, c := range r.Cols {
		typ := "string"
		if c.Kind == rastrillo.Money {
			typ = "*op.Money"
		}
		fmt.Fprintf(&b, "%s %s `json:%q`\n", c.Name, typ, wire(c.Name))
	}
	b.WriteString("CreatedAt string `json:\"created_at\"`\nUpdatedAt string `json:\"updated_at\"`\n}\n\n")

	// ListInput, Page, ListResult.
	b.WriteString(`// ListInput selects one page. A PageNumber from 1 is the page door's
// numbered pagination, and fills Page.Total; with PageNumber 0 the page
// starts after the cursor After, which is how the API pages.
type ListInput struct {
`)
	if r.SearchBind {
		b.WriteString("Search string\n")
	}
	for _, fv := range r.Filters {
		fmt.Fprintf(&b, "%s string\n", fv.Param)
	}
	b.WriteString("After string\nPageNumber int\nLimit int\n}\n\n")
	fmt.Fprintf(&b, "// Page is one page of %s. NextAfter is empty on the last page.\ntype Page struct {\nItems []%s\nNextAfter string\nTotal int64\n}\n\n", r.HumanMany, r.Singular)
	fmt.Fprintf(&b, "// ListResult is a page as the API answers it.\ntype ListResult struct {\n%s []%s `json:%q`\nNextAfter *string `json:\"next_after\"`\n}\n\n", r.Plural, r.Singular, r.Name)
	fmt.Fprintf(&b, `// Result is p as the API answers it: next_after is null on the last page.
func (p Page) Result() ListResult {
	r := ListResult{%s: p.Items}
	if p.NextAfter != "" {
		r.NextAfter = &p.NextAfter
	}
	return r
}

`, r.Plural)

	// CreateInput and UpdateInput: the same fields, so a hand-written
	// Update can take exactly what Create does.
	b.WriteString(`// CreateInput and UpdateInput carry every form field with whether it
// was sent, so an update changes only what was sent and a required zero
// is not mistaken for a missing value.
`)
	for _, name := range []string{"CreateInput", "UpdateInput"} {
		fmt.Fprintf(&b, "type %s struct {\n", name)
		for _, f := range r.Fields {
			fmt.Fprintf(&b, "%s %s `json:%q`\n", f.Name, inputType(f), f.Wire)
		}
		b.WriteString("}\n\n")
	}

	// The argument types.
	b.WriteString(`// ListArgs, GetArgs, UpdateArgs and DeleteArgs are what
// Descriptor.Invoke decodes and the descriptors' Input schemas describe.
type ListArgs struct {
`)
	if r.SearchBind {
		b.WriteString("Q string `json:\"q,omitempty\"`\n")
	}
	for _, fv := range r.Filters {
		fmt.Fprintf(&b, "%s string `json:\"%s,omitempty\"`\n", fv.Param, wire(fv.Field))
	}
	b.WriteString("After string `json:\"after,omitempty\"`\nLimit int `json:\"limit,omitempty\"`\n}\n\n")
	b.WriteString("type GetArgs struct {\nID int64 `json:\"id\"`\n}\n\n")
	b.WriteString("type UpdateArgs struct {\nID int64 `json:\"id\"`\nUpdateInput `json:\",inline\"`\n}\n\n")
	b.WriteString("type DeleteArgs struct {\nID int64 `json:\"id\"`\n}\n\n")

	// The operations.
	b.WriteString("var (\n")
	for _, o := range []struct{ name, action, access string }{
		{"ListOp", "list", "Read"}, {"GetOp", "get", "Read"}, {"CreateOp", "create", "Write"}, {"UpdateOp", "update", "Write"}, {"DeleteOp", "delete", "Write"},
	} {
		fmt.Fprintf(&b, "%s = rastrillo.Operation{ID: %q, Resource: %q, Scoped: %t, Access: rastrillo.%s}\n", o.name, r.Name+"."+o.action, r.Name, r.Scoped, o.access)
	}
	b.WriteString(")\n\n")

	b.WriteString(validators(r))
	if money {
		b.WriteString(moneyHelpers(r))
	}
	b.WriteString(rowHelpers(r))
	b.WriteString(schemas(r))
	b.WriteString(descriptors(r))
	return b.String()
}

// inputType is the Opt a form field decodes into.
func inputType(f field) string {
	switch {
	case f.Optional:
		return "op.Nullable[op.Money]"
	case f.Money:
		return "op.Opt[op.Money]"
	}
	return "op.Opt[string]"
}

// validators renders ValidateCreate and ValidateUpdate. The required
// message is the English form.Parse gives a text field today, computed
// here and emitted as a literal, so both doors say the same words.
func validators(r resource) string {
	var b strings.Builder
	b.WriteString(`// ValidateCreate is Create's checks with no write. A page door whose
// form did not parse calls it instead of Create and shows both sets of
// problems at once. The map is never nil, and empty when in passes.
func ValidateCreate(in CreateInput) map[string]string {
	errs := map[string]string{}
`)
	for _, f := range r.Fields {
		required := fmt.Sprintf("%s is required", f.Human)
		switch {
		case f.Optional:
			fmt.Fprintf(&b, "if in.%[1]s.Set && !in.%[1]s.Null {\nif msg := checkMoney(in.%[1]s.Value); msg != \"\" {\nerrs[%[1]q] = msg\n}\n}\n", f.Name)
		case f.Money:
			fmt.Fprintf(&b, "if !in.%[1]s.Set {\nerrs[%[1]q] = %[2]q\n} else if msg := checkMoney(in.%[1]s.Value); msg != \"\" {\nerrs[%[1]q] = msg\n}\n", f.Name, required)
		case f.Required:
			fmt.Fprintf(&b, "if strings.TrimSpace(in.%[1]s.Value) == \"\" {\nerrs[%[1]q] = %[2]q\n}\n", f.Name, required)
		}
	}
	b.WriteString("return errs\n}\n\n")
	b.WriteString(`// ValidateUpdate is Update's checks with no write, for the same reason.
// A field that was not sent is not checked.
func ValidateUpdate(in UpdateInput) map[string]string {
	errs := map[string]string{}
`)
	for _, f := range r.Fields {
		switch {
		case f.Optional:
			fmt.Fprintf(&b, "if in.%[1]s.Set && !in.%[1]s.Null {\nif msg := checkMoney(in.%[1]s.Value); msg != \"\" {\nerrs[%[1]q] = msg\n}\n}\n", f.Name)
		case f.Money:
			fmt.Fprintf(&b, "if in.%[1]s.Set {\nif msg := checkMoney(in.%[1]s.Value); msg != \"\" {\nerrs[%[1]q] = msg\n}\n}\n", f.Name)
		case f.Required:
			fmt.Fprintf(&b, "if in.%[1]s.Set && strings.TrimSpace(in.%[1]s.Value) == \"\" {\nerrs[%[1]q] = %[2]q\n}\n", f.Name, fmt.Sprintf("%s is required", f.Human))
		}
	}
	b.WriteString("return errs\n}\n\n")
	return b.String()
}

// moneyHelpers renders checkMoney and settle. Their messages are
// English rather than catalog keys: the API answers them as they are,
// and the page never shows them because its own form.Parse money checks
// run first.
func moneyHelpers(r resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, `// checkMoney is what an amount must be to be stored: in a currency the
// storage set has, and not below zero, as the page's money field
// requires. It answers "" for an amount that passes.
func checkMoney(m op.Money) string {
	if m.Currency != "" {
		if _, err := currency.Normalize(m.Currency); err != nil {
			return %q
		}
	}
	if m.Amount < 0 {
		return "Enter an amount of 0 or more."
	}
	return ""
}

`, fmt.Sprintf("Use a currency code this app supports, like %s.", r.Currency))
	b.WriteString(comment(fmt.Sprintf("settle is the amount and currency to store: the field's own currency when it has one, else the one sent, else %s, the resource's default. An update never changes a currency that is set (that is the change-currency operation's job), so a different one is op.Conflict.", r.Currency)))
	fmt.Fprintf(&b, `func settle(name string, m op.Money, held *string) (*int64, *string, error) {
	code := %q
	if held != nil {
		code = *held
	}
	if m.Currency != "" {
		c, err := currency.Normalize(m.Currency)
		if err != nil {
			return nil, nil, err
		}
		if held != nil && c != *held {
			return nil, nil, op.Conflict{Message: fmt.Sprintf("%%s is in %%s. Send the amount in %%s, or leave out currency.", name, *held, *held)}
		}
		code = c
	}
	amount := m.Amount
	return &amount, &code, nil
}

`, r.Currency)
	return b.String()
}

// rowHelpers renders load, fromRow, moneyOf (money only) and items.
func rowHelpers(r resource) string {
	var b strings.Builder
	if r.Scoped {
		b.WriteString(comment(fmt.Sprintf("load reads one of the caller's %s without a check of its own: Get, Create and Update have made theirs, and a policy that allows a write but not a read must not turn a stored write into an error.", r.HumanMany)))
	} else {
		b.WriteString(comment(fmt.Sprintf("load reads one %s without a check of its own: Get, Create and Update have made theirs, and a policy that allows a write but not a read must not turn a stored write into an error.", r.HumanOne)))
	}
	fmt.Fprintf(&b, `func load(ctx context.Context, rc *rastrillo.Ctx, id int64) (%[1]s, error) {
	row, err := %[2]s.New(rc.DB).Get%[1]s(ctx, %[3]s)
	if errors.Is(err, sql.ErrNoRows) {
		return %[1]s{}, op.NotFound{}
	}
	if err != nil {
		return %[1]s{}, err
	}
	return fromRow(row), nil
}

func fromRow(row %[2]s.%[1]s) %[1]s {
	return %[1]s{
		ID: row.ID,
`, r.Singular, r.Alias, r.recordArg("Get"))
	for _, c := range r.Cols {
		if c.Kind == rastrillo.Money {
			fmt.Fprintf(&b, "%[1]s: moneyOf(row.%[1]sMinor, row.%[1]sCurrency),\n", c.Name)
		} else {
			fmt.Fprintf(&b, "%[1]s: row.%[1]s,\n", c.Name)
		}
	}
	b.WriteString("CreatedAt: row.CreatedAt,\nUpdatedAt: row.UpdatedAt,\n}\n}\n\n")
	if r.resultMoney() {
		b.WriteString(`// moneyOf is a stored amount, or nil for an empty field. Boot refuses a
// row that has one of the pair without the other.
func moneyOf(minor *int64, cur *string) *op.Money {
	if minor == nil || cur == nil {
		return nil
	}
	return &op.Money{Amount: *minor, Currency: *cur}
}

`)
	}
	fmt.Fprintf(&b, `// items is never nil, so an empty page answers [] and not null.
func items(rows []%[2]s.%[1]s) []%[1]s {
	out := make([]%[1]s, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row))
	}
	return out
}

`, r.Singular, r.Alias)
	return b.String()
}

// JSON Schema fragments. They are strings rather than a schema library
// because the shapes are five and fixed, and a reader of the generated
// file should see the contract as the API answers it.
const (
	moneyInSchema         = `{"type":"object","properties":{"amount":{"type":"integer"},"currency":{"type":"string"}},"required":["amount"],"additionalProperties":false}`
	optionalMoneyInSchema = `{"type":["object","null"],"properties":{"amount":{"type":"integer"},"currency":{"type":"string"}},"required":["amount"],"additionalProperties":false}`
	moneyOutSchema        = `{"type":["object","null"],"properties":{"amount":{"type":"integer"},"currency":{"type":"string"}},"required":["amount","currency"]}`
	stringSchema          = `{"type":"string"}`
	integerSchema         = `{"type":"integer"}`
	dateTimeSchema        = `{"type":"string","format":"date-time"}`
)

// schemas renders the schema constants. A property that is money is
// spliced in by name, so the file reads as the shapes it reuses.
func schemas(r resource) string {
	var b strings.Builder
	b.WriteString(`// The descriptors' JSON Schemas: the arguments as Invoke decodes them
// and the results as the API door answers them.
const (
`)
	if r.inputMoney() {
		fmt.Fprintf(&b, "moneyInSchema = `%s`\n", moneyInSchema)
		if r.optionalMoney() {
			fmt.Fprintf(&b, "optionalMoneyInSchema = `%s`\n", optionalMoneyInSchema)
		}
	}
	if r.resultMoney() {
		fmt.Fprintf(&b, "moneyOutSchema = `%s`\n", moneyOutSchema)
	}

	// listArgsSchema
	var props []string
	if r.SearchBind {
		props = append(props, `"q":`+stringSchema)
	}
	declared, values, hasDeclared := shape.DeclaredFilter(r.Resource)
	for _, fv := range r.Filters {
		if hasDeclared && fv.Field == declared {
			props = append(props, fmt.Sprintf(`"%s":{"type":"string","enum":[%s]}`, wire(fv.Field), quotedList(values)))
		} else {
			props = append(props, fmt.Sprintf(`"%s":%s`, wire(fv.Field), stringSchema))
		}
	}
	props = append(props, `"after":`+stringSchema, `"limit":{"type":"integer","minimum":1,"maximum":200}`)
	fmt.Fprintf(&b, "listArgsSchema = `{\"type\":\"object\",\"properties\":{%s},\"additionalProperties\":false}`\n", strings.Join(props, ","))
	b.WriteString("idArgsSchema = `{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"integer\"}},\"required\":[\"id\"],\"additionalProperties\":false}`\n")

	// createArgsSchema and updateArgsSchema. A money property is a
	// spliced constant, which breaks the raw string around it.
	var inProps []string
	var required []string
	for _, f := range r.Fields {
		switch {
		case f.Optional:
			inProps = append(inProps, fmt.Sprintf("\"%s\":` + optionalMoneyInSchema + `", f.Wire))
		case f.Money:
			inProps = append(inProps, fmt.Sprintf("\"%s\":` + moneyInSchema + `", f.Wire))
		default:
			inProps = append(inProps, fmt.Sprintf(`"%s":%s`, f.Wire, stringSchema))
		}
		if f.Required {
			required = append(required, f.Wire)
		}
	}
	fmt.Fprintf(&b, "createArgsSchema = `{\"type\":\"object\",\"properties\":{%s},\"required\":[%s],\"additionalProperties\":false}`\n", strings.Join(inProps, ","), quotedList(required))
	// The id leads the update properties as one more entry, so a form
	// with no fields leaves no comma behind it.
	updateProps := append([]string{`"id":` + integerSchema}, inProps...)
	fmt.Fprintf(&b, "updateArgsSchema = `{\"type\":\"object\",\"properties\":{%s},\"required\":[\"id\"],\"additionalProperties\":false}`\n", strings.Join(updateProps, ","))

	// resultSchema and listResultSchema.
	outProps := []string{`"id":` + integerSchema}
	outRequired := []string{"id"}
	for _, c := range r.Cols {
		w := wire(c.Name)
		if c.Kind == rastrillo.Money {
			outProps = append(outProps, fmt.Sprintf("\"%s\":` + moneyOutSchema + `", w))
		} else {
			outProps = append(outProps, fmt.Sprintf(`"%s":%s`, w, stringSchema))
		}
		outRequired = append(outRequired, w)
	}
	outProps = append(outProps, `"created_at":`+dateTimeSchema, `"updated_at":`+dateTimeSchema)
	outRequired = append(outRequired, "created_at", "updated_at")
	fmt.Fprintf(&b, "resultSchema = `{\"type\":\"object\",\"properties\":{%s},\"required\":[%s]}`\n", strings.Join(outProps, ","), quotedList(outRequired))
	fmt.Fprintf(&b, "listResultSchema = `{\"type\":\"object\",\"properties\":{\"%s\":{\"type\":\"array\",\"items\":` + resultSchema + `},\"next_after\":{\"type\":[\"string\",\"null\"]}},\"required\":[\"%s\",\"next_after\"]}`\n", r.Name, r.Name)
	b.WriteString("noResultSchema = `{\"type\":\"null\"}`\n)\n\n")
	return b.String()
}

// quotedList is a JSON array's members: "a","b".
func quotedList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ",")
}

// descriptors renders Descriptors. Each Invoke decodes with
// op.DecodeInput, the API door's rules, so the two cannot drift.
func descriptors(r resource) string {
	var b strings.Builder
	b.WriteString(comment(fmt.Sprintf("Descriptors names every %s operation, for the API tree now and MCP later. Each Invoke decodes its arguments with the API door's rules and calls the operation.", r.Name)))
	b.WriteString("func Descriptors() []op.Descriptor {\nreturn []op.Descriptor{\n")

	listDesc := fmt.Sprintf("List %s, newest first.", r.HumanMany)
	if r.Scoped {
		listDesc = fmt.Sprintf("List your %s, newest first.", r.HumanMany)
	}
	var listIn []string
	if r.SearchBind {
		listIn = append(listIn, "Search: a.Q")
	}
	for _, fv := range r.Filters {
		listIn = append(listIn, fmt.Sprintf("%[1]s: a.%[1]s", fv.Param))
	}
	listIn = append(listIn, "After: a.After", "Limit: a.Limit")
	fmt.Fprintf(&b, `{
	Operation: ListOp,
	Exposed: %[1]t,
	Description: %[2]q,
	Input: json.RawMessage(listArgsSchema),
	Result: json.RawMessage(listResultSchema),
	Invoke: func(ctx context.Context, rc *rastrillo.Ctx, args json.RawMessage) (any, error) {
		var a ListArgs
		if err := op.DecodeInput(args, &a); err != nil {
			return nil, err
		}
		p, err := List(ctx, rc, ListInput{%[3]s})
		if err != nil {
			return nil, err
		}
		return p.Result(), nil
	},
},
`, r.Exposed(), listDesc, strings.Join(listIn, ", "))
	fmt.Fprintf(&b, `{
	Operation: GetOp,
	Exposed: %t,
	Description: %q,
	Input: json.RawMessage(idArgsSchema),
	Result: json.RawMessage(resultSchema),
	Invoke: func(ctx context.Context, rc *rastrillo.Ctx, args json.RawMessage) (any, error) {
		var a GetArgs
		if err := op.DecodeInput(args, &a); err != nil {
			return nil, err
		}
		return Get(ctx, rc, a.ID)
	},
},
`, r.Exposed(), fmt.Sprintf("Get one %s by id.", r.HumanOne))
	fmt.Fprintf(&b, `{
	Operation: CreateOp,
	Exposed: %t,
	Description: %q,
	Input: json.RawMessage(createArgsSchema),
	Result: json.RawMessage(resultSchema),
	Invoke: func(ctx context.Context, rc *rastrillo.Ctx, args json.RawMessage) (any, error) {
		var a CreateInput
		if err := op.DecodeInput(args, &a); err != nil {
			return nil, err
		}
		return Create(ctx, rc, a)
	},
},
`, r.Exposed(), fmt.Sprintf("Create %s %s.", r.Article, r.HumanOne))
	fmt.Fprintf(&b, `{
	Operation: UpdateOp,
	Exposed: %t,
	Description: %q,
	Confirm: %q,
	Input: json.RawMessage(updateArgsSchema),
	Result: json.RawMessage(resultSchema),
	Invoke: func(ctx context.Context, rc *rastrillo.Ctx, args json.RawMessage) (any, error) {
		var a UpdateArgs
		if err := op.DecodeInput(args, &a); err != nil {
			return nil, err
		}
		return Update(ctx, rc, a.ID, a.UpdateInput)
	},
},
`, r.Exposed(), fmt.Sprintf("Change %s %s. Send only the fields to change.", r.Article, r.HumanOne), fmt.Sprintf("Change %s {id}?", r.HumanOne))
	fmt.Fprintf(&b, `{
	Operation: DeleteOp,
	Exposed: %t,
	Description: %q,
	Confirm: %q,
	Input: json.RawMessage(idArgsSchema),
	Result: json.RawMessage(noResultSchema),
	Invoke: func(ctx context.Context, rc *rastrillo.Ctx, args json.RawMessage) (any, error) {
		var a DeleteArgs
		if err := op.DecodeInput(args, &a); err != nil {
			return nil, err
		}
		return nil, Delete(ctx, rc, a.ID)
	},
},
`, r.Exposed(), fmt.Sprintf("Delete %s %s.", r.Article, r.HumanOne), fmt.Sprintf("Delete %s {id}?", r.HumanOne))
	b.WriteString("}\n}\n")
	return b.String()
}
