package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	rastrillo "github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/eventlog"
)

// eventPayload is what each event stores. A created event carries only
// fields (its row id is derived from the event's own coordinates);
// updated and deleted name the row explicitly.

// mergeableEngine is the event-sourced storage half (§5): commands
// append to the resource's stream — created / updated / deleted, the
// updated payload carrying only its own section's fields, so the
// Basics/Advanced invariant holds in the history itself — and the rows
// are a pure Derive fold over the merged stream. Row ids are
// "<writer>-<seq>" of the creating event: unique across edges without
// coordination, which is the point of the shape.
//
// Reads fold the whole stream. That is the honest v1 trade: replay is
// exact, convergence is inherited from eventlog's deterministic merge,
// and a snapshot cache is an optimization to add when a real stream is
// slow, not before.
type mergeableEngine struct {
	log *eventlog.Log
	res rastrillo.Resource
}

type eventPayload struct {
	ID     string         `json:"id,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

func (m *mergeableEngine) stream() string { return "resource/" + m.res.Name }

// derive folds the stream into live rows, newest-created first.
func (m *mergeableEngine) derive() ([]map[string]any, error) {
	events, err := m.log.Events(context.Background(), m.stream())
	if err != nil {
		return nil, err
	}
	type rowState struct {
		row   map[string]any
		order int // creation position in merge order, for stable sorting
	}
	rows := map[string]*rowState{}
	for i, ev := range events {
		var p eventPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return nil, fmt.Errorf("screens: stream %s event %s/%d: %w", m.stream(), ev.Writer, ev.Seq, err)
		}
		switch ev.Kind {
		case "created":
			// The row id IS the creating event's coordinates — unique
			// across edges without coordination. The payload never
			// carries it; every reader derives it identically.
			id := fmt.Sprintf("%s-%d", ev.Writer, ev.Seq)
			row := map[string]any{"ID": id, "CreatedAt": ev.TS, "UpdatedAt": ev.TS}
			m.applyFields(row, p.Fields)
			rows[id] = &rowState{row: row, order: i}
		case "updated":
			if st, ok := rows[p.ID]; ok {
				m.applyFields(st.row, p.Fields)
				st.row["UpdatedAt"] = ev.TS
			}
		case "deleted":
			delete(rows, p.ID) // the tombstone: derive skips dead ids
		}
	}
	out := make([]*rowState, 0, len(rows))
	for _, st := range rows {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].order > out[j].order })
	result := make([]map[string]any, len(out))
	for i, st := range out {
		result[i] = st.row
	}
	return result, nil
}

// applyFields writes payload fields into a row with kind-correct types
// (JSON numbers arrive as float64; Money must go back to int64 cents).
func (m *mergeableEngine) applyFields(row map[string]any, fields map[string]any) {
	for name, v := range fields {
		f, ok := m.res.FieldByName(name)
		if !ok {
			continue
		}
		switch f.Kind {
		case rastrillo.Money, rastrillo.Meter:
			if n, ok := v.(float64); ok {
				row[name] = int64(n)
			}
		case rastrillo.Bool:
			if b, ok := v.(bool); ok {
				row[name] = b
			}
		default:
			if s, ok := v.(string); ok {
				row[name] = s
			}
		}
	}
}

func (m *mergeableEngine) list(q string, filters map[string]string, page int) ([]map[string]any, int, error) {
	rows, err := m.derive()
	if err != nil {
		return nil, 0, err
	}
	var kept []map[string]any
	for _, row := range rows {
		if q != "" && !m.matchesSearch(row, q) {
			continue
		}
		if !m.matchesFilters(row, filters) {
			continue
		}
		kept = append(kept, row)
	}
	total := len(kept)
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := min(start+pageSize, total)
	return kept[start:end], total, nil
}

func (m *mergeableEngine) matchesSearch(row map[string]any, q string) bool {
	q = strings.ToLower(q)
	for _, f := range m.res.Fields() {
		if f.Kind != rastrillo.Text && f.Kind != rastrillo.LongText {
			continue
		}
		if s, ok := row[f.Name].(string); ok && strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

func (m *mergeableEngine) matchesFilters(row map[string]any, filters map[string]string) bool {
	for _, name := range m.res.List.Filter {
		v, ok := filters[name]
		if !ok || v == "" {
			continue
		}
		f, _ := m.res.FieldByName(name)
		if f.Kind == rastrillo.Bool {
			want := v == "1" || v == "true"
			if got, _ := row[name].(bool); got != want {
				return false
			}
		} else if got, _ := row[name].(string); got != v {
			return false
		}
	}
	return true
}

func (m *mergeableEngine) get(id string) (map[string]any, error) {
	rows, err := m.derive()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row["ID"] == id {
			return row, nil
		}
	}
	return nil, errRowNotFound
}

func (m *mergeableEngine) create(vals map[string]any, actor string) (string, error) {
	ev, err := m.log.Append(context.Background(), m.stream(), "created", actor, eventPayload{Fields: vals})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d", ev.Writer, ev.Seq), nil
}

func (m *mergeableEngine) update(id string, vals map[string]any, actor string) error {
	if _, err := m.get(id); err != nil {
		return err
	}
	_, err := m.log.Append(context.Background(), m.stream(), "updated", actor, eventPayload{ID: id, Fields: vals})
	return err
}

func (m *mergeableEngine) delete(id string, actor string) error {
	if _, err := m.get(id); err != nil {
		return err
	}
	_, err := m.log.Append(context.Background(), m.stream(), "deleted", actor, eventPayload{ID: id})
	return err
}
