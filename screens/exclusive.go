package screens

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	rastrillo "github.com/carlosframework/rastrillo"
)

// exclusiveEngine is the plain-rows storage half: the blog store's
// proven SQL shape (parameterized everywhere, escaped LIKE search,
// COUNT+page pagination, RFC3339 UTC timestamps), generated from the
// manifest instead of hand-written per resource.
type exclusiveEngine struct {
	db  *sql.DB
	res rastrillo.Resource
}

func (e *exclusiveEngine) columns() []string {
	cols := []string{"id"}
	for _, f := range e.res.Fields() {
		cols = append(cols, rastrillo.SnakeCase(f.Name))
	}
	return append(cols, "created_at", "updated_at")
}

// scan reads one row in columns() order into the field-name map.
func (e *exclusiveEngine) scan(row interface{ Scan(...any) error }) (map[string]any, error) {
	fields := e.res.Fields()
	dest := make([]any, 0, len(fields)+3)
	var id int64
	dest = append(dest, &id)
	raws := make([]any, len(fields))
	for i, f := range fields {
		switch f.Kind {
		case rastrillo.Bool, rastrillo.Money, rastrillo.Meter:
			raws[i] = new(int64)
		default:
			raws[i] = new(string)
		}
		dest = append(dest, raws[i])
	}
	var created, updated string
	dest = append(dest, &created, &updated)

	if err := row.Scan(dest...); err != nil {
		return nil, err
	}

	out := map[string]any{"ID": strconv.FormatInt(id, 10)}
	for i, f := range fields {
		switch f.Kind {
		case rastrillo.Bool:
			out[f.Name] = *(raws[i].(*int64)) != 0
		case rastrillo.Money, rastrillo.Meter:
			out[f.Name] = *(raws[i].(*int64))
		default:
			out[f.Name] = *(raws[i].(*string))
		}
	}
	for name, v := range map[string]string{"CreatedAt": created, "UpdatedAt": updated} {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			out[name] = t
		} else {
			out[name] = time.Time{}
		}
	}
	return out, nil
}

// searchable returns the columns a q= search covers: Text and LongText.
func (e *exclusiveEngine) searchable() []string {
	var out []string
	for _, f := range e.res.Fields() {
		if f.Kind == rastrillo.Text || f.Kind == rastrillo.LongText {
			out = append(out, rastrillo.SnakeCase(f.Name))
		}
	}
	return out
}

// where builds the WHERE clause for a search + filters, parameterized.
func (e *exclusiveEngine) where(q string, filters map[string]string) (string, []any) {
	var conds []string
	var args []any
	if q != "" {
		var likes []string
		for _, col := range e.searchable() {
			likes = append(likes, col+` LIKE ? ESCAPE '\'`)
			args = append(args, likePattern(q))
		}
		if len(likes) > 0 {
			conds = append(conds, "("+strings.Join(likes, " OR ")+")")
		}
	}
	for _, name := range e.res.List.Filter {
		v, ok := filters[name]
		if !ok || v == "" {
			continue
		}
		f, _ := e.res.FieldByName(name)
		if f.Kind == rastrillo.Bool {
			n := 0
			if v == "1" || v == "true" {
				n = 1
			}
			conds = append(conds, rastrillo.SnakeCase(name)+" = ?")
			args = append(args, n)
		} else {
			conds = append(conds, rastrillo.SnakeCase(name)+" = ?")
			args = append(args, v)
		}
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (e *exclusiveEngine) list(q string, filters map[string]string, page int) ([]map[string]any, int, error) {
	where, args := e.where(q, filters)

	var total int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM `+e.res.Name+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + strings.Join(e.columns(), ", ") + ` FROM ` + e.res.Name + where +
		` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	rows, err := e.db.Query(query, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		m, err := e.scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (e *exclusiveEngine) get(id string) (map[string]any, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, errRowNotFound
	}
	m, err := e.scan(e.db.QueryRow(
		`SELECT `+strings.Join(e.columns(), ", ")+` FROM `+e.res.Name+` WHERE id = ?`, n))
	if err == sql.ErrNoRows {
		return nil, errRowNotFound
	}
	return m, err
}

func (e *exclusiveEngine) create(vals map[string]any, _ string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var cols []string
	var marks []string
	var args []any
	for _, f := range storedFields(e.res.Fields()) {
		if v, ok := vals[f.Name]; ok {
			cols = append(cols, rastrillo.SnakeCase(f.Name))
			marks = append(marks, "?")
			args = append(args, sqlValue(f, v))
		}
	}
	cols = append(cols, "created_at", "updated_at")
	marks = append(marks, "?", "?")
	args = append(args, now, now)

	res, err := e.db.Exec(`INSERT INTO `+e.res.Name+` (`+strings.Join(cols, ", ")+`)
		VALUES (`+strings.Join(marks, ", ")+`)`, args...)
	if err != nil {
		return "", err
	}
	id, err := res.LastInsertId()
	return strconv.FormatInt(id, 10), err
}

// update writes exactly the fields present in vals — the mechanism
// behind the Basics/Advanced invariant: each save's vals carry only its
// own section's fields, so it structurally cannot clobber the other's.
func (e *exclusiveEngine) update(id string, vals map[string]any, _ string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return errRowNotFound
	}
	var sets []string
	var args []any
	for _, f := range storedFields(e.res.Fields()) {
		if v, ok := vals[f.Name]; ok {
			sets = append(sets, rastrillo.SnakeCase(f.Name)+" = ?")
			args = append(args, sqlValue(f, v))
		}
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339), n)
	res, err := e.db.Exec(`UPDATE `+e.res.Name+` SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return errRowNotFound
	}
	return nil
}

func (e *exclusiveEngine) delete(id string, _ string) error {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return errRowNotFound
	}
	_, err = e.db.Exec(`DELETE FROM `+e.res.Name+` WHERE id = ?`, n)
	return err
}

// sqlValue converts a parsed form value to its column representation.
func sqlValue(f rastrillo.Field, v any) any {
	if f.Kind == rastrillo.Bool {
		if b, _ := v.(bool); b {
			return 1
		}
		return 0
	}
	return v
}

// likePattern wraps a search string for LIKE with ESCAPE '\' — about
// meaning, not injection (the value is bound): "100%" should match
// literally (the blog store's rule).
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}
