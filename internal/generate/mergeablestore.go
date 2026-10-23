// Package generate's mergeable store emitter (this file) renders the
// second store shape a manifest resource can declare (store =
// "mergeable", spec 2026-08-22): gen/store/<name>/store.go +
// migrations.go, template-rendered Go over the eventlog package — no
// sqlc involvement at all. Its reads keep the sqlc-generated exclusive
// stores' surface (List/Count/Get, the same Params struct spellings,
// sql.ErrNoRows on a miss), so the generated templates, locales and
// router ship unchanged. Its writes do not: Create, Update and Delete
// take the caller's actor, which sqlc's never do, because an event
// records who wrote it and a row does not; Update and Delete report the
// records they touched (0 or 1), decided under the write lock. Delete
// appends a tombstone event rather than a DELETE (the §9 ruling), and
// derive skips dead ids.
//
// Observable semantics deliberately mirrored from the exclusive
// emitters (store.go's whereClauses/queriesSQL/recordKey):
//   - the always-on owner clause for a scoped resource (no
//     empty-disables-it escape), and id-AND-owner keying on every
//     per-record read/write — a foreign row is sql.ErrNoRows, the
//     404-not-403 discipline for free;
//   - search as a case-insensitive substring over the text/textarea
//     list columns, empty search matching everything;
//   - one exact-match clause per filter field, empty value disabling
//     it;
//   - List's ORDER BY created_at DESC, id DESC and LIMIT/OFFSET
//     window;
//   - sqlc's own argument conventions: a Params struct at two or more
//     bind parameters, the one value passed bare at exactly one, no
//     argument at zero (see actionIndexGET's countBinds switch).
package generate

import (
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
)

// replayStream is Appendix C.2's op.replay.stream, naming the stream a
// fold failed on. %w prints what the approved %v prints, and keeps the
// replay error matchable with errors.Is.
const replayStream = "%s: %w"

// mergeableFieldLines is one model or Params field: money is
// <F>Minor *int64 and <F>Currency *string, the names the operations
// read and write for every store kind (nil in both is an empty field),
// and everything else a string.
func mergeableFieldLines(name string, k rastrillo.Kind) string {
	if k == rastrillo.Money {
		return name + "Minor *int64\n" + name + "Currency *string\n"
	}
	return name + " string\n"
}

// mergeableMoney is r's money columns: what its events carry beside an
// amount, and what its reads fold through replay.
func mergeableMoney(r rastrillo.Resource) []column {
	var out []column
	for _, c := range columns(r) {
		if c.Kind == rastrillo.Money {
			out = append(out, c)
		}
	}
	return out
}

func mergeableCountBinds(r rastrillo.Resource) int { return shape.CountBinds(r) }

// wrapComment wraps at the width this package's goldens pin.
func wrapComment(text string) string { return shape.WrapComment(text, 70) }

// mergeableMatchesDoc builds the matches doc comment from the clauses
// the resource actually declares, mirroring whereClauses' own order.
func mergeableMatchesDoc(r rastrillo.Resource, searchBind bool) string {
	var parts []string
	if scoped(r) {
		parts = append(parts, "the always-on owner clause leads (a scoped list that forgot its owner would be everyone's list)")
	}
	if searchBind {
		parts = append(parts, "an empty search matches everything, otherwise a case-insensitive substring over the searchable list columns (SQLite LIKE's own ASCII fold)")
	}
	if len(filterVars(r)) > 0 {
		parts = append(parts, "each declared filter is an exact-match clause an empty value disables")
	}
	return wrapComment("matches mirrors the exclusive store's WHERE clauses: " + strings.Join(parts, "; ") + ".")
}

// mergeableStoreGo renders gen/store/<name>/store.go for a mergeable
// resource. Unformatted; EmitStore runs it through format.Source.
func mergeableStoreGo(r rastrillo.Resource) []byte {
	model := singularPascal(r.Name)
	plural := pascalCase(r.Name)
	pkg := r.Name + "store"
	cols := columns(r)
	fvars := filterVars(r)
	searchBind := r.List.Search && len(searchColumns(r)) > 0
	hasClauses := scoped(r) || searchBind || len(fvars) > 0
	money := mergeableMoney(r)
	syncImport, currencyImport := "", ""
	if len(money) > 0 {
		syncImport, currencyImport = "\t\"sync\"\n", "\t\"amadan.net/rastrillo/rastrillo/currency\"\n"
	}

	// matchesParams / matchesArgs are the clause bind parameters in
	// whereClauses order (owner, search, filters); Args is the
	// arg.-qualified spelling a Params-struct call site uses.
	var matchesParams, matchesArgs []string
	if scoped(r) {
		matchesParams = append(matchesParams, "owner")
		matchesArgs = append(matchesArgs, "arg.Owner")
	}
	if searchBind {
		matchesParams = append(matchesParams, "search")
		matchesArgs = append(matchesArgs, "arg.Search")
	}
	for _, fv := range fvars {
		matchesParams = append(matchesParams, fv.Var)
		matchesArgs = append(matchesArgs, "arg."+fv.Param)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `// Code generated by rastrillo generate; DO NOT EDIT.

// Package %[2]s is the mergeable store for the
// %[1]s resource: the exclusive (sqlc) stores' exact method
// surface, implemented over the eventlog package. Every record is one
// event stream ("%[1]s/<id>"); a create/update appends an
// event, a delete appends a tombstone, and every read derives live
// state by replaying the merged history — no table of this resource's
// own, no materialized cache (replay is cheap until proven otherwise).
//
// Every write takes its actor from the caller, rastrillo.Actor's
// String(), so each event says who wrote it. One recorded caveat,
// deliberate: record ids are writer-local. Create takes the next id
// under the eventlog's write lock and appends the record's first event
// under the same lock (eventlog.AppendUnder), so two creates on one
// database never share an id; keeping ids apart across writers belongs
// to the platform transport design, and this allocator is the first
// thing it replaces.
package %[2]s

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
%[3]s
%[4]s	"amadan.net/rastrillo/rastrillo/currency/replay"
	"amadan.net/rastrillo/rastrillo/eventlog"
)

// StreamPrefix namespaces this resource's streams in the shared
// eventlog table: record <id> lives at "%[1]s/<id>".
const StreamPrefix = "%[1]s/"

`, r.Name, pkg, syncImport, currencyImport)
	b.WriteString(wrapComment("errGone stops an append to a record that is " + gone(r) + ". It is decided inside the append's transaction, on the history the event would land on, so a delete that commits first is seen."))
	fmt.Fprintf(&b, "var errGone = errors.New(%q)\n\n", pkg+": no such record")

	// The model struct — the same shape sqlc generates.
	fmt.Fprintf(&b, "// %s is the derived read model — the same shape sqlc\n// generates for an exclusive resource.\n", model)
	fmt.Fprintf(&b, "type %s struct {\n", model)
	b.WriteString("ID int64\n")
	if scoped(r) {
		b.WriteString("Owner string\n")
	}
	for _, c := range cols {
		b.WriteString(mergeableFieldLines(c.Name, c.Kind))
	}
	b.WriteString("CreatedAt string\nUpdatedAt string\n}\n\n")

	b.WriteString(`type Queries struct {
	db *sql.DB
}

func New(db *sql.DB) *Queries {
	return &Queries{db: db}
}

`)
	// A store without money still folds through replay: its rules are
	// empty, so only the version is checked, and an event from a newer
	// writer is refused rather than half read. A store with money also
	// needs the upgrade boundary, which never changes once the rename
	// wrote it, so it is read once per database handle and kept, one
	// small entry per handle, as eventlog keeps its payload column.
	const openDoc = "// open resolves the database's durable writer identity (lazily, per\n// call — a one-row SELECT) and binds an eventlog.Log to it.\n"
	if len(money) == 0 {
		b.WriteString(openDoc + `func (q *Queries) open(ctx context.Context) (*eventlog.Log, replay.Rules, error) {
	writer, err := eventlog.LocalWriter(ctx, q.db)
	if err != nil {
		return nil, replay.Rules{}, err
	}
	log, err := eventlog.Open(q.db, writer)
	return log, replay.Rules{}, err
}

`)
	} else {
		b.WriteString("var fields = []replay.Money{\n")
		for _, c := range money {
			fmt.Fprintf(&b, "{Field: %q, AmountKey: %q, CurrencyKey: %q},\n", c.Name, c.SQL, c.SQL+"_currency")
		}
		b.WriteString("}\n\n")
		if r.Legacy != nil {
			fmt.Fprintf(&b, "var legacy = &currency.Legacy{Currency: %q, Scale: %d}\n\n", r.Legacy.Currency, *r.Legacy.Scale)
		} else {
			b.WriteString("var legacy *currency.Legacy\n\n")
		}
		b.WriteString("var rulesByDB sync.Map\n\n" + openDoc + `func (q *Queries) open(ctx context.Context) (*eventlog.Log, replay.Rules, error) {
	writer, err := eventlog.LocalWriter(ctx, q.db)
	if err != nil {
		return nil, replay.Rules{}, err
	}
	log, err := eventlog.Open(q.db, writer)
	if err != nil {
		return nil, replay.Rules{}, err
	}
	if r, ok := rulesByDB.Load(q.db); ok {
		return log, r.(replay.Rules), nil
	}
	boundary, err := eventlog.UpgradeBoundary(ctx, q.db)
	if err != nil {
		return nil, replay.Rules{}, err
	}
	r := replay.Rules{Fields: fields, Legacy: legacy, Boundary: boundary, HasBoundary: true}
	rulesByDB.Store(q.db, r)
	return log, r, nil
}

`)
	}
	// The version member is EventVersion, not V: a text field V is a
	// member too, and the two did not compile side by side. The JSON
	// key stays "V", the key every event already written carries, and
	// Validate refuses a declared EventVersion.
	b.WriteString(`// payload is the JSON shape of every created/updated event: pointer
// fields, omitted when nil, so a created event carries every field and
// an updated event only the fields it changes; an absent field leaves
// the derived value untouched.
type payload struct {
EventVersion int ` + "`json:\"V\"`" + `
`)
	if scoped(r) {
		b.WriteString("Owner *string `json:\"owner,omitempty\"`\n")
	}
	// Money is not in the struct: its keys are written as a map and read
	// only by replay, whose exact key match a second, case-insensitive
	// struct decode could disagree with.
	for _, c := range cols {
		if c.Kind != rastrillo.Money {
			fmt.Fprintf(&b, "%s *string `json:%q`\n", c.Name, c.SQL+",omitempty")
		}
	}
	b.WriteString("CreatedAt *string `json:\"created_at,omitempty\"`\nUpdatedAt *string `json:\"updated_at,omitempty\"`\n}\n\n")

	// The fold. Every event goes through rules.Apply first, the version
	// before the kind and after a tombstone too, so a newer writer's
	// event is refused wherever it sits. Amounts come only from Apply:
	// the struct decode below reads the other fields. A failure returns
	// no record at all, never the part folded so far.
	fmt.Fprintf(&b, `// derive folds one stream's merged history into its live row; ok is
// false for an empty or tombstoned stream.
func derive(rules replay.Rules, id int64, events []eventlog.Event) (%[1]s, map[string]replay.Field, bool, error) {
	rec := map[string]replay.Field{}
	if len(events) == 0 {
		return %[1]s{}, rec, false, nil
	}
	var row %[1]s
	dead := false
	for i, ev := range events {
		if err := rules.Apply(rec, i, ev); err != nil {
			return %[1]s{}, nil, false, fmt.Errorf(%[3]q, ev.Stream, err)
		}
		if dead {
			continue
		}
		switch ev.Kind {
		case "created", "updated":
			var p payload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				return %[1]s{}, nil, false, fmt.Errorf("%[2]s: decode %%s event on %%s: %%w", ev.Kind, ev.Stream, err)
			}
`, model, pkg, replayStream)
	if scoped(r) {
		b.WriteString("if p.Owner != nil {\nrow.Owner = *p.Owner\n}\n")
	}
	for _, c := range cols {
		if c.Kind != rastrillo.Money {
			fmt.Fprintf(&b, "if p.%[1]s != nil {\nrow.%[1]s = *p.%[1]s\n}\n", c.Name)
		}
	}
	// "created" also seeds updated_at from created_at, the same
	// both-timestamps-at-once rule the exclusive INSERT uses.
	b.WriteString(`if p.CreatedAt != nil {
row.CreatedAt = *p.CreatedAt
row.UpdatedAt = *p.CreatedAt
}
if p.UpdatedAt != nil {
row.UpdatedAt = *p.UpdatedAt
}
		case "deleted":
			dead = true
		}
	}
`)
	fmt.Fprintf(&b, "if dead {\nreturn %s{}, rec, false, nil\n}\nrow.ID = id\n", model)
	for _, c := range money {
		fmt.Fprintf(&b, "row.%[1]sMinor, row.%[1]sCurrency = rec[%[1]q].Minor, rec[%[1]q].Currency\n", c.Name)
	}
	b.WriteString("return row, rec, true, nil\n}\n\n")

	if len(money) > 0 {
		fmt.Fprintf(&b, `// encode is p as stored JSON with amounts merged in: a map, because an
// explicit null (a cleared field) is what a struct with omitempty cannot
// write.
func encode(p payload, amounts map[string]any) (json.RawMessage, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(amounts) == 0 {
		return raw, nil
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	for k, v := range amounts {
		if all[k], err = json.Marshal(v); err != nil {
			return nil, err
		}
	}
	return json.Marshal(all)
}

// fold replays history and then the event about to be appended through
// the money rules. An event the fold refuses (an amount without its
// currency, a changed currency) would otherwise be written for good,
// and every later read of the stream, and every boot, would fail on it.
// Callers run it inside AppendChecked, on the history as it is under the
// write lock: a history read earlier let a racing writer clear the field
// and refill it in another currency in between.
func fold(rules replay.Rules, stream string, history []eventlog.Event, kind string, raw json.RawMessage) error {
	rec := map[string]replay.Field{}
	for i, ev := range append(history, eventlog.Event{Stream: stream, Kind: kind, Payload: raw}) {
		if err := rules.Apply(rec, i, ev); err != nil {
			return fmt.Errorf(%[1]q, stream, err)
		}
	}
	return nil
}

`, replayStream)
	}
	b.WriteString(mergeableAppendLiveGo(r))

	// Every mergeable store, money or not: Boot replays each one so an
	// event from a newer writer stops the start rather than failing the
	// first request that reads its stream (spec §7).
	b.WriteString(`func CheckReplay(ctx context.Context, db *sql.DB) error {
	_, err := New(db).live(ctx)
	return err
}

`)

	fmt.Fprintf(&b, `// streamID parses the record id out of a stream name.
func streamID(stream string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(stream, StreamPrefix), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%[2]s: bad stream %%q: %%w", stream, err)
	}
	return id, nil
}

// live derives every live record, sorted the way the exclusive List
// orders rows (created_at DESC, id DESC — RFC3339 strings compare
// correctly as text, exactly as they do in SQLite).
func (q *Queries) live(ctx context.Context) ([]%[1]s, error) {
	log, rules, err := q.open(ctx)
	if err != nil {
		return nil, err
	}
	events, err := log.EventsByPrefix(ctx, StreamPrefix)
	if err != nil {
		return nil, err
	}
	byID := map[int64][]eventlog.Event{}
	var ids []int64
	for _, ev := range events {
		id, err := streamID(ev.Stream)
		if err != nil {
			return nil, err
		}
		if _, ok := byID[id]; !ok {
			ids = append(ids, id)
		}
		byID[id] = append(byID[id], ev)
	}
	var rows []%[1]s
	for _, id := range ids {
		row, _, ok, err := derive(rules, id, byID[id])
		if err != nil {
			return nil, err
		}
		if ok {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt != rows[j].CreatedAt {
			return rows[i].CreatedAt > rows[j].CreatedAt
		}
		return rows[i].ID > rows[j].ID
	})
	return rows, nil
}

`, model, pkg)

	if hasClauses {
		b.WriteString(mergeableMatchesDoc(r, searchBind))
		fmt.Fprintf(&b, "func matches(n %s, %s string) bool {\n", model, strings.Join(matchesParams, ", "))
		if scoped(r) {
			b.WriteString("if n.Owner != owner {\nreturn false\n}\n")
		}
		if searchBind {
			b.WriteString("if search != \"\" {\ns := strings.ToLower(search)\n")
			var terms []string
			for _, c := range searchColumns(r) {
				terms = append(terms, fmt.Sprintf("strings.Contains(strings.ToLower(n.%s), s)", c.Name))
			}
			if len(terms) == 1 {
				fmt.Fprintf(&b, "if !%s {\nreturn false\n}\n", terms[0])
			} else {
				fmt.Fprintf(&b, "if !(%s) {\nreturn false\n}\n", strings.Join(terms, " || "))
			}
			b.WriteString("}\n")
		}
		for _, fv := range fvars {
			fmt.Fprintf(&b, "if %[1]s != \"\" && n.%[2]s != %[1]s {\nreturn false\n}\n", fv.Var, fv.Field)
		}
		b.WriteString("return true\n}\n\n")
	}

	fmt.Fprintf(&b, `// pageWindow applies the SQL LIMIT/OFFSET page window.
func pageWindow(rows []%[1]s, offset, limit int64) []%[1]s {
	if offset < 0 {
		offset = 0
	}
	if offset >= int64(len(rows)) {
		return nil
	}
	rows = rows[offset:]
	if limit >= 0 && limit < int64(len(rows)) {
		rows = rows[:limit]
	}
	return rows
}

`, model)

	// ── List ───────────────────────────────────────────────────────────
	fmt.Fprintf(&b, "type List%sParams struct {\n", plural)
	if scoped(r) {
		b.WriteString("Owner string\n")
	}
	if searchBind {
		b.WriteString("Search string\n")
	}
	for _, fv := range fvars {
		fmt.Fprintf(&b, "%s string\n", fv.Param)
	}
	b.WriteString("PageOffset int64\nPageLimit int64\n}\n\n")

	fmt.Fprintf(&b, "func (q *Queries) List%[1]s(ctx context.Context, arg List%[1]sParams) ([]%[2]s, error) {\n", plural, model)
	b.WriteString("rows, err := q.live(ctx)\nif err != nil {\nreturn nil, err\n}\n")
	if hasClauses {
		fmt.Fprintf(&b, "var out []%s\nfor _, n := range rows {\nif matches(n, %s) {\nout = append(out, n)\n}\n}\n", model, strings.Join(matchesArgs, ", "))
		b.WriteString("return pageWindow(out, arg.PageOffset, arg.PageLimit), nil\n}\n\n")
	} else {
		b.WriteString("return pageWindow(rows, arg.PageOffset, arg.PageLimit), nil\n}\n\n")
	}

	// ── List after a cursor ────────────────────────────────────────────
	// A keyset page rather than an offset: an offset shifts when a row
	// before it is created or deleted between two pages, and the client
	// then skips or repeats a row.
	fmt.Fprintf(&b, "type List%sAfterParams struct {\n", plural)
	if scoped(r) {
		b.WriteString("Owner string\n")
	}
	if searchBind {
		b.WriteString("Search string\n")
	}
	for _, fv := range fvars {
		fmt.Fprintf(&b, "%s string\n", fv.Param)
	}
	b.WriteString("AfterCreatedAt string\nAfterID int64\nPageLimit int64\n}\n\n")
	b.WriteString(wrapComment(fmt.Sprintf("List%sAfter is the page after the row (AfterCreatedAt, AfterID) in List's order; an empty AfterCreatedAt starts at the top. The cursor carries that row's own values, so it still works after the row is deleted.", plural)))
	fmt.Fprintf(&b, "func (q *Queries) List%[1]sAfter(ctx context.Context, arg List%[1]sAfterParams) ([]%[2]s, error) {\n", plural, model)
	fmt.Fprintf(&b, "rows, err := q.live(ctx)\nif err != nil {\nreturn nil, err\n}\nvar out []%s\nfor _, n := range rows {\nif int64(len(out)) == arg.PageLimit {\nbreak\n}\n", model)
	if hasClauses {
		fmt.Fprintf(&b, "if !matches(n, %s) {\ncontinue\n}\n", strings.Join(matchesArgs, ", "))
	}
	b.WriteString("if arg.AfterCreatedAt != \"\" && (n.CreatedAt > arg.AfterCreatedAt || n.CreatedAt == arg.AfterCreatedAt && n.ID >= arg.AfterID) {\ncontinue\n}\nout = append(out, n)\n}\nreturn out, nil\n}\n\n")

	// ── Count ──────────────────────────────────────────────────────────
	switch binds := mergeableCountBinds(r); {
	case binds == 0:
		fmt.Fprintf(&b, "func (q *Queries) Count%s(ctx context.Context) (int64, error) {\n", plural)
		b.WriteString("rows, err := q.live(ctx)\nif err != nil {\nreturn 0, err\n}\nreturn int64(len(rows)), nil\n}\n\n")
	case binds == 1:
		// sqlc's one-bind convention: the value passed bare, never a
		// one-field Params struct (see actionIndexGET's countBinds doc).
		fmt.Fprintf(&b, "func (q *Queries) Count%s(ctx context.Context, %s string) (int64, error) {\n", plural, matchesParams[0])
		b.WriteString("rows, err := q.live(ctx)\nif err != nil {\nreturn 0, err\n}\n")
		fmt.Fprintf(&b, "var total int64\nfor _, n := range rows {\nif matches(n, %s) {\ntotal++\n}\n}\nreturn total, nil\n}\n\n", matchesParams[0])
	default:
		fmt.Fprintf(&b, "type Count%sParams struct {\n", plural)
		if scoped(r) {
			b.WriteString("Owner string\n")
		}
		if searchBind {
			b.WriteString("Search string\n")
		}
		for _, fv := range fvars {
			fmt.Fprintf(&b, "%s string\n", fv.Param)
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "func (q *Queries) Count%[1]s(ctx context.Context, arg Count%[1]sParams) (int64, error) {\n", plural)
		b.WriteString("rows, err := q.live(ctx)\nif err != nil {\nreturn 0, err\n}\n")
		fmt.Fprintf(&b, "var total int64\nfor _, n := range rows {\nif matches(n, %s) {\ntotal++\n}\n}\nreturn total, nil\n}\n\n", strings.Join(matchesArgs, ", "))
	}

	// ── Get ────────────────────────────────────────────────────────────
	if scoped(r) {
		fmt.Fprintf(&b, "type Get%sParams struct {\nID int64\nOwner string\n}\n\n", model)
		fmt.Fprintf(&b, `func (q *Queries) Get%[1]s(ctx context.Context, arg Get%[1]sParams) (%[1]s, error) {
	log, rules, err := q.open(ctx)
	if err != nil {
		return %[1]s{}, err
	}
	events, err := log.Events(ctx, StreamPrefix+strconv.FormatInt(arg.ID, 10))
	if err != nil {
		return %[1]s{}, err
	}
	row, _, ok, err := derive(rules, arg.ID, events)
	if err != nil {
		return %[1]s{}, err
	}
	if !ok || row.Owner != arg.Owner {
		// A foreign row answers exactly like a missing one — the scope
		// package's 404-not-403 discipline.
		return %[1]s{}, sql.ErrNoRows
	}
	return row, nil
}

`, model)
	} else {
		fmt.Fprintf(&b, `func (q *Queries) Get%[1]s(ctx context.Context, id int64) (%[1]s, error) {
	log, rules, err := q.open(ctx)
	if err != nil {
		return %[1]s{}, err
	}
	events, err := log.Events(ctx, StreamPrefix+strconv.FormatInt(id, 10))
	if err != nil {
		return %[1]s{}, err
	}
	row, _, ok, err := derive(rules, id, events)
	if err != nil {
		return %[1]s{}, err
	}
	if !ok {
		return %[1]s{}, sql.ErrNoRows
	}
	return row, nil
}

`, model)
	}

	// ── Create ─────────────────────────────────────────────────────────
	fmt.Fprintf(&b, "type Create%sParams struct {\n", model)
	if scoped(r) {
		b.WriteString("Owner string\n")
	}
	for _, c := range cols {
		b.WriteString(mergeableFieldLines(c.Name, c.Kind))
	}
	b.WriteString("Now string\n}\n\n")

	rulesVar := "_"
	if len(money) > 0 {
		rulesVar = "rules"
	}
	ownOwner := ""
	if scoped(r) {
		ownOwner = ", each with its own owner"
	}
	// The line breaks are fixed rather than wrapped to the name: the
	// goldens pin this text, and only the first line carries the name.
	fmt.Fprintf(&b, `// Create%[1]s starts a record. eventlog.AppendUnder picks its id and
// appends its first event in one transaction, so two creates at once
// make two records%[3]s.
func (q *Queries) Create%[1]s(ctx context.Context, actor string, arg Create%[1]sParams) (int64, error) {
	log, %[2]s, err := q.open(ctx)
	if err != nil {
		return 0, err
	}
	p := payload{
`, model, rulesVar, ownOwner)
	if scoped(r) {
		b.WriteString("Owner: &arg.Owner,\n")
	}
	b.WriteString("EventVersion: replay.Version,\n")
	for _, c := range cols {
		if c.Kind != rastrillo.Money {
			fmt.Fprintf(&b, "%[1]s: &arg.%[1]s,\n", c.Name)
		}
	}
	b.WriteString("CreatedAt: &arg.Now,\n}\n")
	if len(money) == 0 {
		b.WriteString("return create(ctx, log, actor, p)\n}\n\n")
	} else {
		// A created record with no amount writes neither key, which
		// replay reads as "still empty", as it reads history written
		// before the field existed; a pair of nulls would clear nothing.
		b.WriteString("amounts := map[string]any{}\n")
		for _, c := range money {
			fmt.Fprintf(&b, "if arg.%[1]sMinor != nil || arg.%[1]sCurrency != nil {\namounts[%[2]q], amounts[%[3]q] = arg.%[1]sMinor, arg.%[1]sCurrency\n}\n", c.Name, c.SQL, c.SQL+"_currency")
		}
		b.WriteString("return create(ctx, log, rules, actor, p, amounts)\n}\n\n")
	}

	// The id is one more than the highest under StreamPrefix, read by
	// the append itself under its write lock. Read before the append,
	// two creates running at once took the same id and both wrote
	// "created" to one stream, and the two records read back as one.
	b.WriteString(`// create appends p as a new record's "created" event and returns the
// record's id: one more than the highest under StreamPrefix, which the
// append reads under the same write lock it writes with (see the
// package doc's caveat on ids).
`)
	if len(money) == 0 {
		b.WriteString(`func create(ctx context.Context, log *eventlog.Log, actor string, p payload) (int64, error) {
	var id int64
	_, err := log.AppendUnder(ctx, StreamPrefix, "created", actor, p, func(streams []string) (string, error) {
		next, err := nextID(streams)
		if err != nil {
			return "", err
		}
		id = next
		return StreamPrefix + strconv.FormatInt(id, 10), nil
	})
	return id, err
}

`)
	} else {
		b.WriteString(`func create(ctx context.Context, log *eventlog.Log, rules replay.Rules, actor string, p payload, amounts map[string]any) (int64, error) {
	raw, err := encode(p, amounts)
	if err != nil {
		return 0, err
	}
	var id int64
	_, err = log.AppendUnder(ctx, StreamPrefix, "created", actor, raw, func(streams []string) (string, error) {
		next, err := nextID(streams)
		if err != nil {
			return "", err
		}
		id = next
		stream := StreamPrefix + strconv.FormatInt(id, 10)
		// A new id has no history, so the fold judges the created
		// event alone, as emit's would on an empty stream.
		return stream, fold(rules, stream, nil, "created", raw)
	})
	return id, err
}

`)
	}
	fmt.Fprintf(&b, `// nextID is one more than the highest record id among streams. A
// stream it cannot number is an error, not skipped: skipping it could
// hand out an id beside a stream the allocator cannot see. The largest
// id has no next: plus one wrapped to the smallest, which never beat
// the largest, so every later create took the same wrapped id and
// joined one record.
func nextID(streams []string) (int64, error) {
	var id int64
	for _, s := range streams {
		n, err := streamID(s)
		if err != nil {
			return 0, err
		}
		if n > id {
			id = n
		}
	}
	if id == math.MaxInt64 {
		return 0, errors.New(%[1]q)
	}
	return id + 1, nil
}

`, pkg+": no id is left under "+r.Name+"/")

	// ── Partial update ─────────────────────────────────────────────────
	b.WriteString(mergeablePartialUpdateGo(r, model, cols, money))

	// ── Delete ─────────────────────────────────────────────────────────
	sig, idArg := "id int64", "id"
	if scoped(r) {
		fmt.Fprintf(&b, "type Delete%sParams struct {\nID int64\nOwner string\n}\n\n", model)
		sig, idArg = fmt.Sprintf("arg Delete%sParams", model), "arg.ID, arg.Owner"
		// Fixed breaks, as the golden pins them.
		fmt.Fprintf(&b, "// Delete%s appends a tombstone and reports 1, or 0 when the\n// record is missing, already deleted or someone else's; derive skips\n// dead ids.\n", model)
	} else {
		b.WriteString(wrapComment(fmt.Sprintf("Delete%s appends a tombstone and reports 1, or 0 when the record is missing or already deleted; derive skips dead ids.", model)))
	}
	amountsArg := ""
	if len(money) > 0 {
		amountsArg = ", nil"
	}
	fmt.Fprintf(&b, `func (q *Queries) Delete%[1]s(ctx context.Context, actor string, %[2]s) (int64, error) {
	log, rules, err := q.open(ctx)
	if err != nil {
		return 0, err
	}
	return appendLive(ctx, log, rules, %[3]s, "deleted", actor, payload{EventVersion: replay.Version}%[4]s)
}
`, model, sig, idArg, amountsArg)

	return []byte(b.String())
}

// gone is the records a write reports as 0: a scoped store's foreign
// record answers exactly like a missing one (404-not-403).
func gone(r rastrillo.Resource) string {
	if scoped(r) {
		return "missing, deleted or someone else's"
	}
	return "missing or deleted"
}

// mergeableAppendLiveGo renders appendLive, the append behind Update and
// Delete. Whether the record is live (and the owner's) is decided by
// AppendChecked's check, on the history under the write lock: deciding
// it on a history read first let a delete commit in between, and the
// update then wrote an event after the tombstone.
func mergeableAppendLiveGo(r rastrillo.Resource) string {
	money := len(mergeableMoney(r)) > 0
	var b strings.Builder
	// The three variants the goldens pin keep their hand-set breaks; a
	// scoped store with money has no golden and is wrapped.
	switch {
	case !scoped(r) && !money:
		b.WriteString("// appendLive appends one event to record id's stream and reports 1, or\n// 0 when the record is missing or deleted. Whether it is live is decided\n// inside AppendChecked, on the history the event lands on, so a delete\n// that commits first is seen and nothing is written after it.\n")
	case scoped(r) && !money:
		b.WriteString("// appendLive appends one event to record id's stream and reports 1, or\n// 0 when the record is missing, deleted or not owner's. Whether it is\n// live is decided inside AppendChecked, on the history the event lands\n// on, so a delete that commits first is seen and nothing is written\n// after it.\n")
	case !scoped(r) && money:
		b.WriteString("// appendLive appends one event to record id's stream and reports 1, or\n// 0 when the record is missing or deleted. Whether it is live, and\n// whether the money rules accept the event, are decided inside\n// AppendChecked, on the history the event lands on, so a delete or a\n// currency change that commits first is seen and nothing is written\n// after it.\n")
	default:
		b.WriteString(wrapComment("appendLive appends one event to record id's stream and reports 1, or 0 when the record is missing, deleted or not owner's. Whether it is live, and whether the money rules accept the event, are decided inside AppendChecked, on the history the event lands on, so a delete or a currency change that commits first is seen and nothing is written after it."))
	}
	ownerParam, owned := "", ""
	if scoped(r) {
		ownerParam, owned = "owner, ", " || row.Owner != owner"
	}
	if money {
		fmt.Fprintf(&b, `func appendLive(ctx context.Context, log *eventlog.Log, rules replay.Rules, id int64, %[1]skind, actor string, p payload, amounts map[string]any) (int64, error) {
	stream := StreamPrefix + strconv.FormatInt(id, 10)
	raw, err := encode(p, amounts)
	if err != nil {
		return 0, err
	}
	_, err = log.AppendChecked(ctx, stream, kind, actor, raw, func(history []eventlog.Event) error {
		%[2]s, err := derive(rules, id, history)
		if err != nil {
			return err
		}
		if !ok%[3]s {
			return errGone
		}
		return fold(rules, stream, history, kind, raw)
	})
`, ownerParam, deriveVars(r), owned)
	} else {
		fmt.Fprintf(&b, `func appendLive(ctx context.Context, log *eventlog.Log, rules replay.Rules, id int64, %[1]skind, actor string, p payload) (int64, error) {
	_, err := log.AppendChecked(ctx, StreamPrefix+strconv.FormatInt(id, 10), kind, actor, p, func(history []eventlog.Event) error {
		%[2]s, err := derive(rules, id, history)
		if err != nil {
			return err
		}
		if !ok%[3]s {
			return errGone
		}
		return nil
	})
`, ownerParam, deriveVars(r), owned)
	}
	b.WriteString(`	if errors.Is(err, errGone) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return 1, nil
}

`)
	return b.String()
}

// mergeablePartialUpdateGo renders Update<Singular>Params and
// Update<Singular>: every field optional, so one event carries only
// what the caller sent. A money field needs a third member, <F>Set,
// because nil in both pointers already means "clear it" and a partial
// update must also be able to say "leave it".
func mergeablePartialUpdateGo(r rastrillo.Resource, model string, cols, money []column) string {
	var b strings.Builder
	b.WriteString(wrapComment(fmt.Sprintf("Update%sParams is a partial update: a nil field is left as it is, so the event carries only the fields that were sent.", model)))
	fmt.Fprintf(&b, "type Update%sParams struct {\n", model)
	for _, c := range cols {
		if c.Kind == rastrillo.Money {
			fmt.Fprintf(&b, "// %[1]sSet says %[1]s is part of the update: with both pointers\n// nil it clears the amount, which an absent field cannot say.\n%[1]sSet bool\n%[1]sMinor *int64\n%[1]sCurrency *string\n", c.Name)
			continue
		}
		fmt.Fprintf(&b, "%s *string\n", c.Name)
	}
	b.WriteString("Now string\nID int64\n")
	if scoped(r) {
		b.WriteString("Owner string\n")
	}
	b.WriteString("}\n\n")

	doc := fmt.Sprintf("Update%s appends one \"updated\" event and reports 1, or 0 when the record is %s.", model, gone(r))
	if len(money) > 0 {
		doc += " An amount in a currency other than the one its field holds is refused with replay.ErrCurrencyChange."
	}
	b.WriteString(wrapComment(doc))
	fmt.Fprintf(&b, `func (q *Queries) Update%[1]s(ctx context.Context, actor string, arg Update%[1]sParams) (int64, error) {
	log, rules, err := q.open(ctx)
	if err != nil {
		return 0, err
	}
	p := payload{
EventVersion: replay.Version,
`, model)
	for _, c := range cols {
		if c.Kind != rastrillo.Money {
			fmt.Fprintf(&b, "%[1]s: arg.%[1]s,\n", c.Name)
		}
	}
	b.WriteString("UpdatedAt: &arg.Now,\n}\n")
	ownerArg, amountsArg := "", ""
	if scoped(r) {
		ownerArg = "arg.Owner, "
	}
	if len(money) > 0 {
		b.WriteString("amounts := map[string]any{}\n")
		for _, c := range money {
			fmt.Fprintf(&b, "if arg.%[1]sSet {\namounts[%[2]q], amounts[%[3]q] = arg.%[1]sMinor, arg.%[1]sCurrency\n}\n", c.Name, c.SQL, c.SQL+"_currency")
		}
		amountsArg = ", amounts"
	}
	fmt.Fprintf(&b, "return appendLive(ctx, log, rules, arg.ID, %s\"updated\", actor, p%s)\n}\n\n", ownerArg, amountsArg)
	return b.String()
}

// deriveVars is the left side of a derive call: the row when a scoped
// store checks its owner. The money folded so far is not kept: appendLive
// folds again under the write lock, on the history the event lands on.
func deriveVars(r rastrillo.Resource) string {
	if scoped(r) {
		return "row, _, ok"
	}
	return "_, _, ok"
}

// mergeableSchemaFunc is the function a mergeable store's migrations.go
// reads its statements from. ReadHistory recognises the file by it, so
// the name lives here, beside the emitter, and a rename changes both.
const mergeableSchemaFunc = "eventlogSchemaSQL"

// mergeableMigrationsGo renders gen/store/<name>/migrations.go for a
// mergeable resource: the eventlog schema re-exported — the resource
// has no table of its own. It reads eventlog.Schema rather than the
// eventlog.Migrations []string main emitted, because the migrate
// conversion removed that export; the generated var stays a []string,
// which is what the legacy Options.Migrations path takes.
func mergeableMigrationsGo(r rastrillo.Resource) []byte {
	var b strings.Builder
	b.WriteString("// Code generated by rastrillo generate; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "package %sstore\n\n", r.Name)
	b.WriteString("import \"amadan.net/rastrillo/rastrillo/eventlog\"\n\n")
	b.WriteString("// Migrations re-exports the eventlog schema for Options.Migrations: a\n")
	b.WriteString("// mergeable resource has no table of its own — every record is an\n")
	b.WriteString("// eventlog stream. Idempotent, so two mergeable resources appending it\n")
	b.WriteString("// twice is harmless.\n")
	b.WriteString("//\n")
	b.WriteString("// The statements are read out of eventlog.Schema — the *migrate.Set\n")
	b.WriteString("// the package ships — rather than copied, so this file cannot drift\n")
	b.WriteString("// from the schema migrate.Apply installs for an app on the ledger.\n")
	b.WriteString("//\n")
	b.WriteString("// One element per migration FILE, not per statement, so keep each\n")
	b.WriteString("// file independently idempotent: OpenDB tolerates a duplicate-column\n")
	b.WriteString("// error by skipping the rest of that element, which with a\n")
	b.WriteString("// multi-statement file means skipping every statement after it and\n")
	b.WriteString("// still reporting success.\n")
	fmt.Fprintf(&b, "var Migrations = %s()\n\n", mergeableSchemaFunc)
	fmt.Fprintf(&b, "func %s() []string {\n", mergeableSchemaFunc)
	b.WriteString("\tvar out []string\n")
	b.WriteString("\tfor _, m := range eventlog.Schema.All() {\n")
	b.WriteString("\t\tout = append(out, m.SQL)\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn out\n")
	b.WriteString("}\n")
	return []byte(b.String())
}
