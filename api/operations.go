package api

import (
	"context"
	encodingjson "encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// msgBadQuery is a GET's 422 when its query string does not parse. It
// has no fields, because a pair that does not parse has no sure name.
const msgBadQuery = "Send each argument as name=value. " +
	"Percent-encode the name and value."

// Operations builds the API door for named operations, one route each
// at /api/v1/<area>/<verb>. A read declared InQuery is GET, with its
// input in the query string; every other operation is POST, with its
// input as a JSON body. Pass the routes to New beside any built by hand.
func Operations(descriptors ...op.Descriptor) []Route {
	routes := make([]Route, 0, len(descriptors))
	for _, descriptor := range descriptors {
		route := Route{
			Method: http.MethodPost,
			Pattern: "/api/v1/" +
				strings.Replace(descriptor.ID, ".", "/", 1),
			Op:         descriptor,
			Handle:     door(descriptor),
			readByPost: descriptor.Access == rastrillo.Read,
		}
		if descriptor.InQuery {
			route.Method, route.readByPost = http.MethodGet, false
		}
		routes = append(routes, route)
	}
	return routes
}

// door is a named operation's Handle. It runs op.Check itself, before
// it reads any input, whatever descriptor.Invoke does: a hand-written
// Invoke that skips Check must still never run for a caller the
// operation refuses, and the caller hears that before anything about
// its input.
func door(descriptor op.Descriptor) func(
	context.Context, *rastrillo.Ctx, http.ResponseWriter, *http.Request,
) {
	return func(
		ctx context.Context, rc *rastrillo.Ctx,
		response http.ResponseWriter, request *http.Request,
	) {
		if err := op.Check(ctx, rc, descriptor.Operation); err != nil {
			Refuse(response, request, err)
			return
		}
		arguments, read := doorArguments(response, request, descriptor)
		if !read {
			return
		}
		result, err := descriptor.Invoke(ctx, rc, arguments)
		switch {
		case err != nil:
			Refuse(response, request, err)
		case result == nil:
			response.WriteHeader(http.StatusNoContent)
		default:
			Write(response, http.StatusOK, result)
		}
	}
}

// doorArguments is the request's input as the bytes Invoke reads. They
// are not parsed here: op.DecodeInput alone judges them, as it does for
// MCP, so the two doors refuse a malformed input the same way. On false
// it has already answered.
func doorArguments(
	response http.ResponseWriter, request *http.Request,
	descriptor op.Descriptor,
) ([]byte, bool) {
	if descriptor.InQuery {
		// ParseQuery, not URL.Query, which drops a pair it can't read and
		// would run the operation without that argument.
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			invalid(response, msgBadQuery, nil)
			return nil, false
		}
		return queryArguments(query, descriptor.Input), true
	}
	// No body is {}, as MCP treats absent arguments, so an operation
	// whose inputs are all optional can be called with nothing to send.
	if request.ContentLength == 0 {
		return []byte("{}"), true
	}
	return readJSONBody(response, request)
}

// queryArguments writes a query string as the JSON object a body would
// carry, so op.DecodeInput refuses a repeated, ill-typed or unknown
// parameter with the message it gives a body. Keys are sorted and each
// key's values kept in the order sent, so a repeat reaches DecodeInput
// as a duplicate member. A value goes in raw only when it is already a
// JSON literal of its property's type; everything else is quoted, so a
// query can never write JSON of its own.
func queryArguments(
	query url.Values, inputSchema encodingjson.RawMessage,
) []byte {
	types := propertyTypes(inputSchema)
	object := []byte{'{'}
	for _, key := range slices.Sorted(maps.Keys(query)) {
		for _, value := range query[key] {
			if len(object) > 1 {
				object = append(object, ',')
			}
			object = appendString(object, key)
			object = append(object, ':')
			object = appendArgument(object, value, types[key])
		}
	}
	return append(object, '}')
}

func appendArgument(object []byte, value, propertyType string) []byte {
	numeric := propertyType == "integer" || propertyType == "number"
	switch {
	case numeric && jsonNumber.MatchString(value):
		return append(object, value...)
	case propertyType == "boolean" && (value == "true" || value == "false"):
		return append(object, value...)
	}
	return appendString(object, value)
}

// appendString quotes text. AppendQuote replaces invalid UTF-8, so one
// bad byte is put back, inside the quotes where it can't end the
// string: DecodeInput then refuses the value as it refuses a body that
// carries one, rather than the operation seeing text nobody sent.
func appendString(object []byte, text string) []byte {
	quoted, err := jsontext.AppendQuote(object, text)
	if err != nil {
		return append(quoted[:len(quoted)-1], 0xff, '"')
	}
	return quoted
}

// propertyTypes is each input property's JSON Schema type, where it
// names exactly one. A schema that does not parse names none, so every
// argument is quoted and DecodeInput still judges each one.
func propertyTypes(inputSchema []byte) map[string]string {
	var described struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(inputSchema, &described) != nil {
		return nil
	}
	types := make(map[string]string, len(described.Properties))
	for name, property := range described.Properties {
		if propertyType, named := property.Type.(string); named {
			types[name] = propertyType
		}
	}
	return types
}
