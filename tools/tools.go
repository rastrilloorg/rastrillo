// Package tools is the runtime half of the agents system (design doc
// §8): it renders the generated registry (gen.Tools()) as LLM tool
// schemas, and dispatches a model-proposed call back through the app's
// own mux — "a tool call and an HTTP POST reach the identical Handle
// function" — with every call re-validated against the registry before
// it executes, the caller attributed on Ctx.Actor, and §8's consent
// gate enforced: a write tool refuses to run unconfirmed.
//
// What is deliberately not here: an LLM client. §8 licenses an SDK
// exception case by case, per app — the framework ships the registry,
// schemas, dispatch, consent and the sidecar harness (Options.Sidecar),
// and the app brings whichever model client it answers for. Schemas()
// emits the provider-neutral JSON-Schema shape (name, description,
// input_schema) that current tool-use APIs share.
package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// Schema is one tool's schema, in the common tool-use shape.
type Schema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// Schemas renders the registry for a model. Every argument is a string
// — the same representation the HTTP form layer uses, so nothing is
// invented between the model and the handler.
func Schemas(defs []rastrillo.ToolDef) []Schema {
	out := make([]Schema, 0, len(defs))
	for _, d := range defs {
		props := map[string]any{}
		var required []string
		for name, desc := range d.Args {
			props[name] = map[string]any{"type": "string", "description": desc}
			required = append(required, name)
		}
		sort.Strings(required)
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		desc := d.Description
		if d.Access == rastrillo.ToolWrite {
			desc += " (write: requires human confirmation)"
		}
		out = append(out, Schema{Name: d.ID, Description: desc, InputSchema: schema})
	}
	return out
}

// SchemasJSON is Schemas, marshalled.
func SchemasJSON(defs []rastrillo.ToolDef) ([]byte, error) {
	return json.MarshalIndent(Schemas(defs), "", "  ")
}

// Call is one proposed tool invocation.
type Call struct {
	Tool  string
	Args  map[string]string
	Actor rastrillo.Actor
	// Confirmed asserts a human saw the Confirm sentence and said yes.
	// The dispatcher trusts the app on this — rendering the sentence
	// and collecting the yes is the app's confirm page (§8/§9); what
	// the dispatcher enforces is that an unconfirmed write never runs.
	Confirmed bool
}

var (
	// ErrUnknownTool is a call naming no registry entry — refused, per
	// §8's "re-validated against the same registry before it executes".
	ErrUnknownTool = errors.New("rastrillo/tools: unknown tool")
	// ErrUnknownArg is a call carrying an argument the registry never
	// declared.
	ErrUnknownArg = errors.New("rastrillo/tools: undeclared argument")
	// ErrConfirmRequired is a write tool called without Confirmed — the
	// consent gate. The app should show ConfirmSentence and re-call
	// with Confirmed once a human agrees.
	ErrConfirmRequired = errors.New("rastrillo/tools: write tool requires confirmation")
)

// Result is what the dispatched handler answered.
type Result struct {
	Status int
	Header http.Header
	Body   []byte
}

// ConfirmSentence interpolates a call's arguments into its tool's
// Confirm sentence ("Cancel order {id}?" + {"id":"7"} → "Cancel order
// 7?").
func ConfirmSentence(def rastrillo.ToolDef, args map[string]string) string {
	s := def.Confirm
	for k, v := range args {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// Dispatch validates call against defs and, if it passes, synthesizes
// the HTTP request and runs it through handler — the app's real mux,
// normally the one Serve serves, so middleware, routing and the action
// itself behave exactly as they do for a browser.
func Dispatch(handler http.Handler, defs []rastrillo.ToolDef, call Call) (Result, error) {
	var def *rastrillo.ToolDef
	for i := range defs {
		if defs[i].ID == call.Tool {
			def = &defs[i]
			break
		}
	}
	if def == nil {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownTool, call.Tool)
	}
	for name := range call.Args {
		if _, ok := def.Args[name]; !ok {
			return Result{}, fmt.Errorf("%w: %q for tool %q", ErrUnknownArg, name, call.Tool)
		}
	}
	if def.Access == rastrillo.ToolWrite && !call.Confirmed {
		return Result{}, fmt.Errorf("%w: %q — show %q and re-call confirmed", ErrConfirmRequired, call.Tool, ConfirmSentence(*def, call.Args))
	}

	req, err := buildRequest(*def, call)
	if err != nil {
		return Result{}, err
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return Result{Status: rec.Code, Header: rec.Header(), Body: rec.Body.Bytes()}, nil
}

// buildRequest turns a validated call into the request its action
// expects: path {params} filled from matching args, the rest as form
// values (methods with bodies) or query parameters.
func buildRequest(def rastrillo.ToolDef, call Call) (*http.Request, error) {
	rest := url.Values{}
	path := def.Path
	for name, value := range call.Args {
		placeholder := "{" + name + "}"
		if strings.Contains(def.Path, placeholder) {
			path = strings.ReplaceAll(path, placeholder, url.PathEscape(value))
		} else {
			rest.Set(name, value)
		}
	}
	if strings.Contains(path, "{") {
		return nil, fmt.Errorf("rastrillo/tools: tool %q: path %s still has unfilled parameters — declare them as Args", def.ID, path)
	}

	var req *http.Request
	if def.Method == http.MethodGet || def.Method == http.MethodHead {
		u := path
		if len(rest) > 0 {
			u += "?" + rest.Encode()
		}
		req = httptest.NewRequest(def.Method, u, nil)
	} else {
		req = httptest.NewRequest(def.Method, path, strings.NewReader(rest.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return rastrillo.WithActor(req, call.Actor), nil
}
