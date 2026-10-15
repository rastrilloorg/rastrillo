// Package generate's mergeable store emitter (this file) renders the
// second store shape a manifest resource can declare (store =
// "mergeable", spec 2026-08-22): gen/store/<name>/store.go +
// migrations.go, template-rendered Go over the eventlog package — no
// sqlc involvement at all. The emitted package satisfies the SAME
// method surface the sqlc-generated exclusive stores expose
// (List/Count/Get/Create/UpdateBasics/UpdateAdvanced/Delete, the same
// Params struct spellings, sql.ErrNoRows on a miss), so the generated
// actions, templates, locales and router ship unchanged. The one
// behavioral difference is the §9 ruling: Delete appends a tombstone
// event rather than a DELETE, and derive skips dead ids.
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
)

// replayStream is Appendix C.2's op.replay.stream, naming the stream a
// fold failed on. %w prints what the approved %v prints, and keeps the
// replay error matchable with errors.Is.
const replayStream = "%s: %w"

// mergeableFieldLines is one model or Params field: money is
// <F>Minor *int64 and <F>Currency *string, the names the shared action
// emitter reads and writes for every store kind (nil in both is an
// empty field), and everything else a string.
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

// mergeableAmounts is the money keys an append writes for fields: a
// map, because an explicit null (a cleared field) is what a struct
// with omitempty cannot write.
func mergeableAmounts(fields []column) string {
	var parts []string
	for _, c := range fields {
		parts = append(parts, fmt.Sprintf("%q: arg.%sMinor, %q: arg.%sCurrency", c.SQL, c.Name, c.SQL+"_currency", c.Name))
	}
	if len(parts) == 0 {
		return "nil"
	}
	return "map[string]any{" + strings.Join(parts, ", ") + "}"
}

// mergeableCountBinds counts a resource's Count bind parameters —
// exactly actionIndexGET's countBinds arithmetic, so the emitted
// signature and the generated action's call site cannot drift.
func mergeableCountBinds(r rastrillo.Resource) int {
	binds := len(filterVars(r))
	if r.List.Search && len(searchColumns(r)) > 0 {
		binds++
	}
	if scoped(r) {
		binds++
	}
	return binds
}

// wrapComment word-wraps text into "// " comment lines at the width
// the committed goldens pin.
func wrapComment(text string) string {
	const width = 70
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("// " + l + "\n")
	}
	return b.String()
}

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
// Two recorded caveats, both deliberate:
//   - Every event's actor is stamped "app"; threading Ctx.Actor into
//     store mutations is an additive follow-up (today every generated
//     mutation is consent-gated upstream).
//   - Record ids are writer-local: Create allocates max(existing)+1,
//     race-free under the single-writer SQLite pool both db paths
//     enforce. Cross-writer id namespacing belongs to the platform
//     transport design; this allocator is the first thing it replaces.
package %[2]s

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

// actor is stamped on every event this store appends — see the
// package doc's Ctx.Actor caveat.
const actor = "app"

`, r.Name, pkg, syncImport, currencyImport)

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
// an updated event only its own group's — an absent field leaves the
// derived value untouched.
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
		// emit folds the event onto the stream's money before appending
		// it. An event the fold refuses (an amount without its currency,
		// a changed currency) would otherwise be written for good, and
		// every later read of the stream, and every boot, would fail on
		// it. The fold runs inside AppendChecked, on the history as it
		// is under the write lock: folding a history read earlier let a
		// racing writer clear the field and refill it in another
		// currency in between, and the stale event was appended after.
		fmt.Fprintf(&b, `func emit(ctx context.Context, log *eventlog.Log, rules replay.Rules, stream, kind string, p payload, amounts map[string]any) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(amounts) > 0 {
		var all map[string]json.RawMessage
		if err := json.Unmarshal(raw, &all); err != nil {
			return err
		}
		for k, v := range amounts {
			if all[k], err = json.Marshal(v); err != nil {
				return err
			}
		}
		if raw, err = json.Marshal(all); err != nil {
			return err
		}
	}
	_, err = log.AppendChecked(ctx, stream, kind, actor, json.RawMessage(raw), func(history []eventlog.Event) error {
		rec := map[string]replay.Field{}
		for i, ev := range append(history, eventlog.Event{Stream: stream, Kind: kind, Payload: raw}) {
			if err := rules.Apply(rec, i, ev); err != nil {
				return fmt.Errorf(%[1]q, stream, err)
			}
		}
		return nil
	})
	return err
}

`, replayStream)
	}

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
	fmt.Fprintf(&b, `func (q *Queries) Create%[1]s(ctx context.Context, arg Create%[1]sParams) (int64, error) {
	log, %[2]s, err := q.open(ctx)
	if err != nil {
		return 0, err
	}
	// Writer-local id allocation (see the package doc's caveat):
	// max(existing id in this resource's streams)+1, race-free under
	// the single-writer SQLite pool.
	events, err := log.EventsByPrefix(ctx, StreamPrefix)
	if err != nil {
		return 0, err
	}
	var id int64
	for _, ev := range events {
		evID, err := streamID(ev.Stream)
		if err != nil {
			return 0, err
		}
		if evID > id {
			id = evID
		}
	}
	id++
	p := payload{
`, model, rulesVar)
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
		b.WriteString(`if _, err := log.Append(ctx, StreamPrefix+strconv.FormatInt(id, 10), "created", actor, p); err != nil {
		return 0, err
	}
	return id, nil
}

`)
	} else {
		// A created record with no amount writes neither key, which
		// replay reads as "still empty", as it reads history written
		// before the field existed; a pair of nulls would clear nothing.
		b.WriteString("amounts := map[string]any{}\n")
		for _, c := range money {
			fmt.Fprintf(&b, "if arg.%[1]sMinor != nil || arg.%[1]sCurrency != nil {\namounts[%[2]q], amounts[%[3]q] = arg.%[1]sMinor, arg.%[1]sCurrency\n}\n", c.Name, c.SQL, c.SQL+"_currency")
		}
		b.WriteString(`if err := emit(ctx, log, rules, StreamPrefix+strconv.FormatInt(id, 10), "created", p, amounts); err != nil {
		return 0, err
	}
	return id, nil
}

`)
	}

	// ── Updates ────────────────────────────────────────────────────────
	b.WriteString(mergeableUpdateGo(r, model, r.Form.Basics, "Basics"))
	if len(r.Form.Advanced) > 0 {
		b.WriteString(mergeableUpdateGo(r, model, r.Form.Advanced, "Advanced"))
	}

	// ── Delete ─────────────────────────────────────────────────────────
	sig, idExpr, owned := "id int64", "id", ""
	if scoped(r) {
		fmt.Fprintf(&b, "type Delete%sParams struct {\nID int64\nOwner string\n}\n\n", model)
		sig, idExpr, owned = fmt.Sprintf("arg Delete%sParams", model), "arg.ID", " || row.Owner != arg.Owner"
	}
	fmt.Fprintf(&b, `func (q *Queries) Delete%[1]s(ctx context.Context, %[2]s) error {
	log, rules, err := q.open(ctx)
	if err != nil {
		return err
	}
	stream := StreamPrefix + strconv.FormatInt(%[3]s, 10)
	events, err := log.Events(ctx, stream)
	if err != nil {
		return err
	}
	%[4]s, err := derive(rules, %[3]s, events)
	if err != nil {
		return err
	}
	if !ok%[5]s {
		return nil
	}
	// Delete appends a tombstone event rather than a DELETE; derive
	// skips dead ids from here on.
	%[6]s
}
`, model, sig, idExpr, deriveVars(r), owned, mergeableAppend(len(money) > 0, "deleted", "payload{EventVersion: replay.Version}", "nil"))

	return []byte(b.String())
}

// mergeableUpdateGo renders one Update<Singular><Group> method: the
// exclusive UPDATE's shape (only this group's fields plus the
// updated_at bump, keyed on id — and owner for a scoped resource, so a
// missing or foreign record is a no-op the same way the SQL UPDATE
// affects zero rows).
func mergeableUpdateGo(r rastrillo.Resource, model string, group []rastrillo.Field, groupName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "type Update%s%sParams struct {\n", model, groupName)
	for _, f := range group {
		b.WriteString(mergeableFieldLines(f.Name, f.Kind))
	}
	b.WriteString("Now string\nID int64\n")
	if scoped(r) {
		b.WriteString("Owner string\n")
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, `func (q *Queries) Update%[1]s%[2]s(ctx context.Context, arg Update%[1]s%[2]sParams) error {
	log, rules, err := q.open(ctx)
	if err != nil {
		return err
	}
	stream := StreamPrefix + strconv.FormatInt(arg.ID, 10)
	events, err := log.Events(ctx, stream)
	if err != nil {
		return err
	}
`, model, groupName)
	hasMoney := len(mergeableMoney(r)) > 0
	owned, why := "", "a missing record is a no-op,\n\t\t// never an appended event on a stream with no live record."
	if scoped(r) {
		owned, why = " || row.Owner != arg.Owner", "a missing or foreign record is a\n\t\t// no-op, never an appended event on a stream with no live record."
	}
	fmt.Fprintf(&b, `%s, err := derive(rules, arg.ID, events)
	if err != nil {
		return err
	}
	if !ok%s {
		// Mirror the exclusive UPDATE: %s
		return nil
	}
`, deriveVars(r), owned, why)
	b.WriteString("p := payload{\nEventVersion: replay.Version,\n")
	var groupMoney []column
	for _, f := range group {
		if f.Kind == rastrillo.Money {
			groupMoney = append(groupMoney, column{Name: f.Name, SQL: sqlName(f.Name), Kind: f.Kind})
			continue
		}
		fmt.Fprintf(&b, "%[1]s: &arg.%[1]s,\n", f.Name)
	}
	// A group's money field is written on every save: the amount with
	// its currency, or both null for a blank field, which is how an
	// optional amount is cleared (an absent key means "unchanged").
	fmt.Fprintf(&b, "UpdatedAt: &arg.Now,\n}\n%s\n}\n\n", mergeableAppend(hasMoney, "updated", "p", mergeableAmounts(groupMoney)))
	return b.String()
}

// deriveVars is the left side of a derive call: the row when a scoped
// store checks its owner. The money folded so far is not kept: emit
// folds again under the write lock, on the history the event lands on.
func deriveVars(r rastrillo.Resource) string {
	if scoped(r) {
		return "row, _, ok"
	}
	return "_, _, ok"
}

// mergeableAppend is the statement that appends one event and returns
// its error: through emit, which folds it first, in a store with money.
func mergeableAppend(money bool, kind, p, amounts string) string {
	if money {
		return fmt.Sprintf("return emit(ctx, log, rules, stream, %q, %s, %s)", kind, p, amounts)
	}
	return fmt.Sprintf("_, err = log.Append(ctx, stream, %q, actor, %s)\nreturn err", kind, p)
}

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
	b.WriteString("var Migrations = eventlogSchemaSQL()\n\n")
	b.WriteString("func eventlogSchemaSQL() []string {\n")
	b.WriteString("\tvar out []string\n")
	b.WriteString("\tfor _, m := range eventlog.Schema.All() {\n")
	b.WriteString("\t\tout = append(out, m.SQL)\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn out\n")
	b.WriteString("}\n")
	return []byte(b.String())
}
