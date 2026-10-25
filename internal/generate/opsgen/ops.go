package opsgen

import (
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
)

// listGo renders list.go. The limit defaults to 50 and is capped at
// 200; page mode counts, cursor mode reads one row more than it answers.
func listGo(r resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", r.Pkg)
	std := []string{"context"}
	if r.SearchBind {
		std = append(std, "strings")
	}
	b.WriteString(imports(std, []string{"op"}, &r))
	if r.Scoped {
		b.WriteString(comment(fmt.Sprintf("List answers one page of the caller's %s, newest first. By page number it also counts them, for the page door's numbered links. After a cursor it reads one row more than it returns, so it knows whether there is a next page without counting.", r.HumanMany)))
	} else {
		b.WriteString(comment(fmt.Sprintf("List answers one page of %s, newest first. By page number it also counts them, for the page door's numbered links. After a cursor it reads one row more than it returns, so it knows whether there is a next page without counting.", r.HumanMany)))
	}
	b.WriteString(`func List(ctx context.Context, rc *rastrillo.Ctx, in ListInput) (Page, error) {
	if err := op.Check(ctx, rc, ListOp); err != nil {
		return Page{}, err
	}
	limit := in.Limit
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
`)
	if r.SearchBind {
		b.WriteString("search := strings.TrimSpace(in.Search)\n")
	}
	fmt.Fprintf(&b, "store := %s.New(rc.DB)\nif in.PageNumber > 0 {\n", r.Alias)

	// Count takes its binds the way sqlc passes them: none, one bare,
	// or a Params struct, which is shape.CountBinds's rule.
	switch binds := shape.CountBinds(r.Resource); {
	case binds == 0:
		fmt.Fprintf(&b, "total, err := store.Count%s(ctx)\n", r.Plural)
	case binds == 1:
		var arg string
		switch {
		case r.Scoped:
			arg = "rc.Actor.Subject"
		case r.SearchBind:
			arg = "search"
		default:
			arg = "in." + r.Filters[0].Param
		}
		fmt.Fprintf(&b, "total, err := store.Count%s(ctx, %s)\n", r.Plural, arg)
	default:
		fmt.Fprintf(&b, "total, err := store.Count%[1]s(ctx, %[2]s.Count%[1]sParams{\n%[3]s})\n", r.Plural, r.Alias, r.listBinds())
	}
	fmt.Fprintf(&b, `if err != nil {
		return Page{}, err
	}
	rows, err := store.List%[1]s(ctx, %[2]s.List%[1]sParams{
%[3]sPageOffset: int64((in.PageNumber - 1) * limit),
		PageLimit: int64(limit),
	})
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items(rows), Total: total}, nil
}
afterAt, afterID, ok := op.DecodeCursor(in.After)
if !ok {
	return Page{}, op.Invalid{Fields: map[string]string{"after": "Use the next_after value from the previous page."}}
}
rows, err := store.List%[1]sAfter(ctx, %[2]s.List%[1]sAfterParams{
%[3]sAfterCreatedAt: afterAt,
	AfterID: afterID,
	PageLimit: int64(limit + 1),
})
if err != nil {
	return Page{}, err
}
var next string
if len(rows) > limit {
	rows = rows[:limit]
	next = op.EncodeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
}
return Page{Items: items(rows), NextAfter: next}, nil
}
`, r.Plural, r.Alias, r.listBinds())
	return b.String()
}

// listBinds is the owner, search and filter fields a list or count
// Params struct takes, in the store's order.
func (r resource) listBinds() string {
	var b strings.Builder
	b.WriteString(r.ownerArg())
	if r.SearchBind {
		b.WriteString("Search: search,\n")
	}
	for _, fv := range r.Filters {
		fmt.Fprintf(&b, "%[1]s: in.%[1]s,\n", fv.Param)
	}
	return b.String()
}

// getGo renders get.go.
func getGo(r resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", r.Pkg)
	b.WriteString(imports([]string{"context"}, []string{"op"}, nil))
	if r.Scoped {
		b.WriteString(comment(fmt.Sprintf("Get answers one of the caller's %s. Someone else's is op.NotFound, the same answer as a missing one, so nobody can probe for rows they may not see.", r.HumanMany)))
	} else {
		b.WriteString(comment(fmt.Sprintf("Get answers one %s, or op.NotFound.", r.HumanOne)))
	}
	fmt.Fprintf(&b, `func Get(ctx context.Context, rc *rastrillo.Ctx, id int64) (%[1]s, error) {
	if err := op.Check(ctx, rc, GetOp); err != nil {
		return %[1]s{}, err
	}
	return load(ctx, rc, id)
}
`, r.Singular)
	return b.String()
}

// createGo renders create.go. Text is trimmed, a textarea is stored as
// sent, and money is settled against the manifest's currency.
func createGo(r resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", r.Pkg)
	std := []string{"context"}
	if r.trimsText() {
		std = append(std, "strings")
	}
	std = append(std, "time")
	b.WriteString(imports(std, []string{"op"}, &r))
	doc := "Create checks and stores " + r.Article + " " + r.HumanOne
	if r.Scoped {
		doc += " owned by the caller"
	}
	if r.Mergeable {
		doc += ", recording who asked"
	}
	b.WriteString(comment(doc + ". Both doors call it, so a value one door accepts the other accepts too."))
	fmt.Fprintf(&b, `func Create(ctx context.Context, rc *rastrillo.Ctx, in CreateInput) (%[1]s, error) {
	if err := op.Check(ctx, rc, CreateOp); err != nil {
		return %[1]s{}, err
	}
	if errs := ValidateCreate(in); len(errs) > 0 {
		return %[1]s{}, op.Invalid{Fields: errs}
	}
`, r.Singular)
	for _, f := range r.Fields {
		local := shape.LowerFirst(f.Name)
		switch {
		case f.Optional:
			fmt.Fprintf(&b, `var %[1]sMinor *int64
var %[1]sCurrency *string
if in.%[2]s.Set && !in.%[2]s.Null {
	m, c, err := settle(%[2]q, in.%[2]s.Value, nil)
	if err != nil {
		return %[3]s{}, err
	}
	%[1]sMinor, %[1]sCurrency = m, c
}
`, local, f.Name, r.Singular)
		case f.Money:
			fmt.Fprintf(&b, "%[1]sMinor, %[1]sCurrency, err := settle(%[2]q, in.%[2]s.Value, nil)\nif err != nil {\nreturn %[3]s{}, err\n}\n", local, f.Name, r.Singular)
		}
	}
	fmt.Fprintf(&b, "id, err := %[1]s.New(rc.DB).Create%[2]s(ctx, %[3]s%[1]s.Create%[2]sParams{\n%[4]s", r.Alias, r.Singular, r.actorArg(), r.ownerArg())
	for _, c := range r.Cols {
		f, ok := r.inForm(c)
		if !ok {
			continue
		}
		local := shape.LowerFirst(f.Name)
		switch {
		case f.Money:
			fmt.Fprintf(&b, "%[1]sMinor: %[2]sMinor,\n%[1]sCurrency: %[2]sCurrency,\n", f.Name, local)
		case f.Textarea:
			fmt.Fprintf(&b, "%[1]s: in.%[1]s.Value,\n", f.Name)
		default:
			fmt.Fprintf(&b, "%[1]s: strings.TrimSpace(in.%[1]s.Value),\n", f.Name)
		}
	}
	fmt.Fprintf(&b, `Now: %[2]s,
	})
	if err != nil {
		return %[1]s{}, err
	}
	return load(ctx, rc, id)
}
`, r.Singular, nowExpr)
	return b.String()
}

// updateGo renders update.go: one shape for the exclusive store (read,
// merge and write the whole row in one transaction) and one for the
// mergeable store (one event carrying only the fields sent).
func updateGo(r resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", r.Pkg)
	if len(r.Fields) == 0 {
		// A list-only manifest has nothing a client could send, so an
		// update is a read; the write helpers would be unused and fail
		// the examples' staticcheck, and an empty "nothing sent" test
		// does not parse.
		b.WriteString(imports([]string{"context"}, []string{"op"}, nil))
		b.WriteString(comment(fmt.Sprintf("Update answers the %s as it is: the manifest declares no form field, so there is nothing to change.", r.HumanOne)))
		fmt.Fprintf(&b, `func Update(ctx context.Context, rc *rastrillo.Ctx, id int64, in UpdateInput) (%[1]s, error) {
	if err := op.Check(ctx, rc, UpdateOp); err != nil {
		return %[1]s{}, err
	}
	if errs := ValidateUpdate(in); len(errs) > 0 {
		return %[1]s{}, op.Invalid{Fields: errs}
	}
	return load(ctx, rc, id)
}
`, r.Singular)
		return b.String()
	}
	std := []string{"context"}
	mod := []string{"op"}
	money := r.inputMoney()
	switch {
	case !r.Mergeable:
		std = append(std, "database/sql", "errors")
	case money:
		std = append(std, "errors")
		mod = []string{"currency/replay", "op"}
	}
	if r.trimsText() {
		std = append(std, "strings")
	}
	std = append(std, "time")
	b.WriteString(imports(std, mod, &r))

	if r.Mergeable {
		b.WriteString(comment(fmt.Sprintf("Update changes only the fields that were sent, in one event that names who asked, and answers the %s as it is afterwards. With nothing sent it appends nothing.", r.HumanOne)))
	} else {
		b.WriteString(comment(fmt.Sprintf("Update changes only the fields that were sent and answers the %s as it is afterwards. With nothing sent it writes nothing, so updated_at stays as it was.", r.HumanOne)))
	}
	var unset []string
	for _, f := range r.Fields {
		unset = append(unset, "!in."+f.Name+".Set")
	}
	fmt.Fprintf(&b, `func Update(ctx context.Context, rc *rastrillo.Ctx, id int64, in UpdateInput) (%[1]s, error) {
	if err := op.Check(ctx, rc, UpdateOp); err != nil {
		return %[1]s{}, err
	}
	if errs := ValidateUpdate(in); len(errs) > 0 {
		return %[1]s{}, op.Invalid{Fields: errs}
	}
	if %[2]s {
		return load(ctx, rc, id)
	}
`, r.Singular, strings.Join(unset, " && "))
	if r.Mergeable {
		b.WriteString(mergeableUpdate(r))
		return b.String()
	}
	fmt.Fprintf(&b, `if err := update(ctx, rc, id, in); err != nil {
		return %[1]s{}, err
	}
	return load(ctx, rc, id)
}

// update reads the row, merges what was sent and writes the whole row
// back, in one transaction. That is atomic because rc.DB is the writer
// pool, which has one connection: no other write can run between the
// read and the write. Nothing here may use rc.DB while tx is open, or it
// waits for the connection tx holds.
func update(ctx context.Context, rc *rastrillo.Ctx, id int64, in UpdateInput) error {
	tx, err := rc.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	store := %[2]s.New(tx)
	row, err := store.Get%[1]s(ctx, %[3]s)
	if errors.Is(err, sql.ErrNoRows) {
		return op.NotFound{}
	}
	if err != nil {
		return err
	}
	arg := %[2]s.Update%[1]sParams{
`, r.Singular, r.Alias, r.recordArg("Get"))
	for _, c := range r.Cols {
		if c.Kind == rastrillo.Money {
			fmt.Fprintf(&b, "%[1]sMinor: row.%[1]sMinor,\n%[1]sCurrency: row.%[1]sCurrency,\n", c.Name)
		} else {
			fmt.Fprintf(&b, "%[1]s: row.%[1]s,\n", c.Name)
		}
	}
	fmt.Fprintf(&b, "Now: %s,\nID: id,\n", nowExpr)
	if r.Scoped {
		b.WriteString("Owner: rc.Actor.Subject,\n")
	}
	b.WriteString("}\n")
	for _, f := range r.Fields {
		switch {
		case f.Optional:
			fmt.Fprintf(&b, `if in.%[1]s.Set {
	arg.%[1]sMinor, arg.%[1]sCurrency = nil, nil
	if !in.%[1]s.Null {
		if arg.%[1]sMinor, arg.%[1]sCurrency, err = settle(%[1]q, in.%[1]s.Value, row.%[1]sCurrency); err != nil {
			return err
		}
	}
}
`, f.Name)
		case f.Money:
			fmt.Fprintf(&b, "if in.%[1]s.Set {\nif arg.%[1]sMinor, arg.%[1]sCurrency, err = settle(%[1]q, in.%[1]s.Value, row.%[1]sCurrency); err != nil {\nreturn err\n}\n}\n", f.Name)
		case f.Textarea:
			fmt.Fprintf(&b, "if in.%[1]s.Set {\narg.%[1]s = in.%[1]s.Value\n}\n", f.Name)
		default:
			fmt.Fprintf(&b, "if in.%[1]s.Set {\narg.%[1]s = strings.TrimSpace(in.%[1]s.Value)\n}\n", f.Name)
		}
	}
	fmt.Fprintf(&b, `n, err := store.Update%s(ctx, arg)
	if err != nil {
		return err
	}
	if n == 0 {
		return op.NotFound{}
	}
	return tx.Commit()
}
`, r.Singular)
	return b.String()
}

// mergeableUpdate is Update's body after the empty-input check for a
// mergeable store, plus heldOf when the record carries money.
func mergeableUpdate(r resource) string {
	var b strings.Builder
	money := r.inputMoney()
	if money {
		fmt.Fprintf(&b, `// The record's currencies, for an amount sent without one. The store
// checks again under its write lock, on the history the event lands
// on, so a currency changed since this read is still refused.
cur, err := load(ctx, rc, id)
if err != nil {
	return %s{}, err
}
`, r.Singular)
	}
	fmt.Fprintf(&b, "arg := %s.Update%sParams{\nNow: %s,\nID: id,\n%s}\n", r.Alias, r.Singular, nowExpr, r.ownerArg())
	for _, f := range r.Fields {
		switch {
		case f.Optional:
			fmt.Fprintf(&b, `if in.%[1]s.Set {
	arg.%[1]sSet = true
	if !in.%[1]s.Null {
		if arg.%[1]sMinor, arg.%[1]sCurrency, err = settle(%[1]q, in.%[1]s.Value, heldOf(cur.%[1]s)); err != nil {
			return %[2]s{}, err
		}
	}
}
`, f.Name, r.Singular)
		case f.Money:
			fmt.Fprintf(&b, `if in.%[1]s.Set {
	arg.%[1]sSet = true
	if arg.%[1]sMinor, arg.%[1]sCurrency, err = settle(%[1]q, in.%[1]s.Value, heldOf(cur.%[1]s)); err != nil {
		return %[2]s{}, err
	}
}
`, f.Name, r.Singular)
		case f.Textarea:
			fmt.Fprintf(&b, "if in.%[1]s.Set {\nv := in.%[1]s.Value\narg.%[1]s = &v\n}\n", f.Name)
		default:
			fmt.Fprintf(&b, "if in.%[1]s.Set {\nv := strings.TrimSpace(in.%[1]s.Value)\narg.%[1]s = &v\n}\n", f.Name)
		}
	}
	fmt.Fprintf(&b, "n, err := %s.New(rc.DB).Update%s(ctx, rc.Actor.String(), arg)\n", r.Alias, r.Singular)
	if money {
		fmt.Fprintf(&b, "if errors.Is(err, replay.ErrCurrencyChange) {\nreturn %s{}, op.Conflict{Message: \"A currency changed while this update was saved. Get the record again, then send the amounts in its currencies.\"}\n}\n", r.Singular)
	}
	fmt.Fprintf(&b, `if err != nil {
		return %[1]s{}, err
	}
	if n == 0 {
		return %[1]s{}, op.NotFound{}
	}
	return load(ctx, rc, id)
}
`, r.Singular)
	if money {
		b.WriteString(`
// heldOf is the currency a stored amount is in, or nil for an empty field.
func heldOf(m *op.Money) *string {
	if m == nil {
		return nil
	}
	return &m.Currency
}
`)
	}
	return b.String()
}

// deleteGo renders delete.go.
func deleteGo(r resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", r.Pkg)
	b.WriteString(imports([]string{"context"}, []string{"op"}, &r))
	doc := "Delete removes one " + r.HumanOne
	if r.Scoped {
		doc = "Delete removes one of the caller's " + r.HumanMany
	}
	if r.Mergeable {
		doc += ", recording who asked"
	}
	doc += ". A missing one"
	if r.Scoped {
		doc += ", or someone else's,"
	}
	b.WriteString(comment(doc + " is op.NotFound."))
	fmt.Fprintf(&b, `func Delete(ctx context.Context, rc *rastrillo.Ctx, id int64) error {
	if err := op.Check(ctx, rc, DeleteOp); err != nil {
		return err
	}
	n, err := %s.New(rc.DB).Delete%s(ctx, %s%s)
	if err != nil {
		return err
	}
	if n == 0 {
		return op.NotFound{}
	}
	return nil
}
`, r.Alias, r.Singular, r.actorArg(), r.recordArg("Delete"))
	return b.String()
}
