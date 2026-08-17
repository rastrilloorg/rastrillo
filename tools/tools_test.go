package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	rastrillo "github.com/carlosframework/rastrillo"
)

func registry() []rastrillo.ToolDef {
	return []rastrillo.ToolDef{
		{
			ID: "orders_id_index_get", Method: "GET", Path: "/orders/{id}",
			Tool: rastrillo.Tool{
				Description: "Read one order.",
				Args:        map[string]string{"id": "the order id"},
			},
		},
		{
			ID: "orders_id_cancel_post", Method: "POST", Path: "/orders/{id}/cancel",
			Tool: rastrillo.Tool{
				Description: "Cancel one order.",
				Access:      rastrillo.ToolWrite,
				Args:        map[string]string{"id": "the order id", "reason": "why"},
				Confirm:     "Cancel order {id}? Its tickets go back on sale.",
			},
		},
	}
}

// mux is a tiny real router whose handlers echo what reached them.
func mux(t *testing.T, got *map[string]string) http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		(*got)["method"] = r.Method
		(*got)["id"] = r.PathValue("id")
		if a, ok := rastrillo.ActorFromContext(r.Context()); ok {
			(*got)["actor"] = a.String()
		}
		w.Write([]byte("order " + r.PathValue("id")))
	})
	m.HandleFunc("POST /orders/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		(*got)["id"] = r.PathValue("id")
		(*got)["reason"] = r.FormValue("reason")
		if a, ok := rastrillo.ActorFromContext(r.Context()); ok {
			(*got)["actor"] = a.String()
		}
		w.WriteHeader(http.StatusSeeOther)
	})
	return m
}

func TestSchemas(t *testing.T) {
	schemas := Schemas(registry())
	if len(schemas) != 2 {
		t.Fatalf("schemas = %d", len(schemas))
	}
	read := schemas[0]
	if read.Name != "orders_id_index_get" || read.InputSchema["type"] != "object" {
		t.Fatalf("read schema: %+v", read)
	}
	write := schemas[1]
	if !strings.Contains(write.Description, "requires human confirmation") {
		t.Fatalf("write schema must disclose the gate: %+v", write)
	}
	if _, err := SchemasJSON(registry()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchReadTool(t *testing.T) {
	got := map[string]string{}
	res, err := Dispatch(mux(t, &got), registry(), Call{
		Tool:  "orders_id_index_get",
		Args:  map[string]string{"id": "7"},
		Actor: rastrillo.Actor{Name: "concierge"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.Status != 200 || string(res.Body) != "order 7" {
		t.Fatalf("result: %d %q", res.Status, res.Body)
	}
	if got["id"] != "7" || got["actor"] != "agent:concierge" {
		t.Fatalf("handler saw %v — attribution must reach the same Handle a POST reaches", got)
	}
}

func TestDispatchValidatesAgainstTheRegistry(t *testing.T) {
	got := map[string]string{}
	h := mux(t, &got)

	if _, err := Dispatch(h, registry(), Call{Tool: "made_up"}); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("unknown tool: %v", err)
	}
	if _, err := Dispatch(h, registry(), Call{
		Tool: "orders_id_index_get",
		Args: map[string]string{"id": "7", "sneaky": "x"},
	}); !errors.Is(err, ErrUnknownArg) {
		t.Fatalf("undeclared arg: %v", err)
	}
	if len(got) != 0 {
		t.Fatal("a refused call must never reach a handler")
	}
}

func TestDispatchConsentGate(t *testing.T) {
	got := map[string]string{}
	h := mux(t, &got)

	_, err := Dispatch(h, registry(), Call{
		Tool:  "orders_id_cancel_post",
		Args:  map[string]string{"id": "7", "reason": "dup"},
		Actor: rastrillo.Actor{Name: "concierge"},
	})
	if !errors.Is(err, ErrConfirmRequired) {
		t.Fatalf("unconfirmed write: %v", err)
	}
	if !strings.Contains(err.Error(), "Cancel order 7?") {
		t.Fatalf("the gate must carry the interpolated sentence: %v", err)
	}
	if len(got) != 0 {
		t.Fatal("the unconfirmed write ran")
	}

	res, err := Dispatch(h, registry(), Call{
		Tool:      "orders_id_cancel_post",
		Args:      map[string]string{"id": "7", "reason": "dup"},
		Actor:     rastrillo.Actor{Name: "concierge"},
		Confirmed: true,
	})
	if err != nil || res.Status != http.StatusSeeOther {
		t.Fatalf("confirmed write: %v %d", err, res.Status)
	}
	if got["reason"] != "dup" || got["actor"] != "agent:concierge" {
		t.Fatalf("handler saw %v", got)
	}
}

func TestDispatchRefusesUnfilledPathParams(t *testing.T) {
	defs := []rastrillo.ToolDef{{
		ID: "bad", Method: "GET", Path: "/orders/{id}",
		Tool: rastrillo.Tool{Description: "no args declared"},
	}}
	if _, err := Dispatch(http.NewServeMux(), defs, Call{Tool: "bad"}); err == nil {
		t.Fatal("an unfillable path must refuse, not dispatch a literal {id}")
	}
}
