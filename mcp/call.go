package mcp

import (
	"bytes"
	"context"
	encodingjson "encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

// calling is the handler for descriptor's tool. Every answer is a tool
// result, a refusal included, so the model can read it and fix the
// call; only an unknown tool stays the SDK's protocol error.
func (caller listing) calling(descriptor op.Descriptor) sdk.ToolHandler {
	return func(
		ctx context.Context, request *sdk.CallToolRequest,
	) (*sdk.CallToolResult, error) {
		return caller.call(ctx, descriptor, request.Params.Arguments), nil
	}
}

// call is one tool call (spec §6), under one recovery: the SDK runs it
// on a goroutine of its own, where a panic would end the process.
func (caller listing) call(
	ctx context.Context, descriptor op.Descriptor,
	arguments encodingjson.RawMessage,
) (result *sdk.CallToolResult) {
	defer func() {
		if value := recover(); value != nil {
			result = caller.refusal(fmt.Errorf("mcp: panic in %s: %v\n%s",
				descriptor.ID, value, debug.Stack()))
		}
	}()
	caller.logCall(descriptor)
	raw := argumentsOf(arguments)
	if !argumentBytesValid(raw) {
		return toolError(api.Error{Code: "invalid", Message: msgArgumentBytes})
	}
	appContext := caller.appContext
	if err := caller.admit(ctx, &appContext, descriptor); err != nil {
		return caller.refusal(err)
	}
	// Kept apart, so the guard reads the arguments as sent even if the
	// operation reuses the bytes it was given.
	sent := slices.Clone(raw)
	value, err := descriptor.Invoke(ctx, &appContext, raw)
	if err != nil {
		return caller.refusal(err)
	}
	encoded, refused := caller.encode(descriptor, value)
	if refused != nil {
		return refused
	}
	caller.guard(descriptor, sent, value)
	return resultOf(encoded)
}

// admit is the call's own Admit and op.Check, so a hand-written Invoke
// that skips op.Check still meets the policy. Once both pass, the gate
// holds every answer to them, a refusal or a panic as much as a result.
func (caller listing) admit(
	ctx context.Context, appContext *rastrillo.Ctx,
	descriptor op.Descriptor,
) error {
	err := caller.served.gate.Admit(appContext.Actor, descriptor.Operation)
	if err != nil {
		return err
	}
	if err := op.Check(ctx, appContext, descriptor.Operation); err != nil {
		return err
	}
	caller.recheckCall(descriptor)
	return nil
}

// encode is the result's one encoding, or the tool error that answers
// instead: a result that will not encode or is not the object the
// tool's schema promises is internal, and one over MaxResult is too
// large.
func (caller listing) encode(
	descriptor op.Descriptor, value any,
) ([]byte, *sdk.CallToolResult) {
	// The result's one encoding; the SDK only copies it. It is the body
	// api.Write gives at the API door, so the text block the agent reads
	// matches it.
	encoded, err := api.Encode(value)
	if err != nil {
		// %v, not %w: whatever a marshaler returned, a result that will
		// not encode is the app's bug, never the caller's refusal.
		return nil, caller.refusal(fmt.Errorf(
			"mcp: encode the result of %s: %v", descriptor.ID, err))
	}
	if !isNullSchema(descriptor.Result) && !bytes.HasPrefix(encoded,
		[]byte("{")) {
		return nil, caller.refusal(fmt.Errorf("mcp: the result of %s "+
			"is not a JSON object, as its Result schema requires",
			descriptor.ID))
	}
	if escapedSize(encoded) > caller.served.config.MaxResult {
		return nil, toolError(api.Error{
			Code: "response_too_large", Message: msgResultTooLarge,
		})
	}
	return encoded, nil
}

// argumentsOf reads absent or null arguments as no arguments, so a tool
// whose members are all optional can be called bare, as clients do.
func argumentsOf(
	arguments encodingjson.RawMessage,
) encodingjson.RawMessage {
	trimmed := bytes.Trim(arguments, jsonSpace)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return encodingjson.RawMessage("{}")
	}
	return arguments
}

// logCall is the tree's "api: request" line for a tool call. It comes
// before Invoke, so a call that panics or hangs still leaves it, and
// the actor goes through the gate's own redactor, as the tree's does.
func (caller listing) logCall(descriptor op.Descriptor) {
	logger, redact := caller.served.gate.Logger, caller.served.gate.Redact
	if logger == nil {
		logger = slog.Default()
	}
	if redact == nil {
		redact = func(text string) string { return text }
	}
	attributes := []slog.Attr{slog.String("tool", ToolName(descriptor.ID))}
	for _, attribute := range caller.appContext.Actor.LogAttrs() {
		attributes = append(attributes, slog.String(
			attribute.Key, redact(attribute.Value.String())))
	}
	logger.LogAttrs(caller.request.Context(), slog.LevelInfo,
		"mcp: call", attributes...)
}

// recheckCall has the gate hold this answer to what the fresh actor may
// still do: Admit and op.Check again for the operation that ran.
func (caller listing) recheckCall(descriptor op.Descriptor) {
	check := func(ctx context.Context, fresh rastrillo.Actor) error {
		err := caller.served.gate.Admit(fresh, descriptor.Operation)
		if err != nil {
			return err
		}
		checked := caller.appContext
		checked.Actor = fresh
		return op.Check(ctx, &checked, descriptor.Operation)
	}
	// On the request's own context, where the gate holds the checks:
	// registered on the SDK's, Disclosed would do nothing.
	api.Disclosed(caller.request.Context(), check)
}

// guard has the gate run the app's Guard, after recheckCall's checks, on
// the arguments sent and the result returned. Only a result has one: a
// refusal carries no rows for Guard to judge.
func (caller listing) guard(
	descriptor op.Descriptor, sent encodingjson.RawMessage, result any,
) {
	guard := caller.served.config.Guard
	if guard == nil {
		return
	}
	api.Disclosed(caller.request.Context(), func(
		ctx context.Context, fresh rastrillo.Actor,
	) error {
		return guard(ctx, fresh, &descriptor, sent, result)
	})
}

// escapedSize is encoded's length once the SDK's encoding/json escapes
// it for HTML, in structuredContent and the text block alike, so a
// result under MaxResult stays within the gate's cap of three results.
func escapedSize(encoded []byte) int {
	size := len(encoded)
	for _, character := range []string{"<", ">", "&"} {
		size += 5 * bytes.Count(encoded, []byte(character))
	}
	// Three bytes in UTF-8, written as a six-byte escape.
	for _, separator := range []rune{0x2028, 0x2029} {
		size += 3 * bytes.Count(encoded, []byte(string(separator)))
	}
	return size
}

// resultOf is the encoded result as both structuredContent and the
// text block. A null result has no structured content, as a tool with
// a null Result schema has no output schema.
func resultOf(encoded []byte) *sdk.CallToolResult {
	text := []sdk.Content{&sdk.TextContent{Text: string(encoded)}}
	if string(encoded) == "null" {
		return &sdk.CallToolResult{Content: text}
	}
	return &sdk.CallToolResult{
		StructuredContent: encodingjson.RawMessage(encoded), Content: text,
	}
}

// refusal is err as the API answers it, logged as the API logs it.
func (caller listing) refusal(err error) *sdk.CallToolResult {
	_, refused := api.Refusal(caller.request, err)
	return toolError(refused)
}

// toolError is the API's error envelope as one text block.
func toolError(refused api.Error) *sdk.CallToolResult {
	// An Error is strings and a map of strings, and with invalid UTF-8
	// allowed they always encode.
	encoded, _ := json.Marshal(map[string]api.Error{"error": refused},
		json.Deterministic(true), jsontext.AllowInvalidUTF8(true))
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: string(encoded)}},
	}
}
