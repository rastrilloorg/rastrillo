package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"

	json "github.com/go-json-experiment/json"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

const methodListTools = "tools/list"

// listing is one request's caller: the request the gate admitted, and
// the app's Ctx for them, a copy of its own.
type listing struct {
	served     *server
	request    *http.Request
	appContext rastrillo.Ctx
}

// narrowing is the receiving middleware that shows a caller only the
// tools they can use, then has the gate check that list again, as it
// was, while it goes out.
func (caller listing) narrowing(next sdk.MethodHandler) sdk.MethodHandler {
	return func(
		ctx context.Context, method string, request sdk.Request,
	) (result sdk.Result, err error) {
		if method != methodListTools {
			return next(ctx, method, request)
		}
		// The SDK runs this on a goroutine of its own, with no recovery,
		// so the gate's recovery cannot reach a panic here.
		defer func() {
			if value := recover(); value != nil {
				result, err = nil, caller.internal(fmt.Errorf(
					"mcp: panic in tools/list: %v\n%s", value, debug.Stack()))
			}
		}()
		result, err = next(ctx, method, request)
		if err != nil {
			return nil, err
		}
		list, isList := result.(*sdk.ListToolsResult)
		if !isList {
			return nil, caller.internal(
				fmt.Errorf("mcp: tools/list answered a %T", result))
		}
		listed := caller.narrow(ctx, list)
		// On the request's own context, where the gate holds the checks:
		// registered anywhere else, Disclosed would do nothing.
		api.Disclosed(caller.request.Context(), caller.recheck(listed))
		return list, nil
	}
}

// narrow keeps the tools this caller passes Admit and op.Check for, and
// returns their descriptors, which the list's recheck holds it to.
func (caller listing) narrow(
	ctx context.Context, list *sdk.ListToolsResult,
) []op.Descriptor {
	tools := []*sdk.Tool{}
	var listed []op.Descriptor
	for _, tool := range list.Tools {
		descriptor, known := caller.served.descriptors[tool.Name]
		if !known || caller.allows(ctx, caller.appContext, descriptor) != nil {
			continue
		}
		tools = append(tools, tool)
		listed = append(listed, descriptor)
	}
	list.Tools = tools
	return listed
}

func (caller listing) allows(
	ctx context.Context, appContext rastrillo.Ctx, descriptor op.Descriptor,
) error {
	err := caller.served.gate.Admit(appContext.Actor, descriptor.Operation)
	if err != nil {
		return err
	}
	return op.Check(ctx, &appContext, descriptor.Operation)
}

// recheck holds the answer to the tools that were listed. Recomputing
// the list instead would quietly drop a tool that became unavailable
// and pass, so any listed tool the fresh actor can no longer use
// refuses the whole answer.
func (caller listing) recheck(listed []op.Descriptor) func(
	ctx context.Context, fresh rastrillo.Actor) error {
	return func(ctx context.Context, fresh rastrillo.Actor) error {
		checked := caller.appContext
		checked.Actor = fresh
		for _, descriptor := range listed {
			if err := caller.allows(ctx, checked, descriptor); err != nil {
				return err
			}
		}
		guard := caller.served.config.Guard
		if guard == nil {
			return nil
		}
		// The descriptors, not the SDK's result, so a guard reads the
		// list in the app's own types.
		return guard(ctx, fresh, nil, nil, listed)
	}
}

// internal is the API's internal error as a JSON-RPC error: logged,
// redacted, under a ref that the answer carries in its data.
func (caller listing) internal(cause error) error {
	_, refused := api.Refusal(caller.request, cause)
	// An Error is strings and a map of strings, which always encode.
	data, _ := json.Marshal(refused)
	return &jsonrpc.Error{
		Code: jsonrpc.CodeInternalError, Message: refused.Message,
		Data: data,
	}
}

// notServedYet answers every tool call until calls are served: an
// internal error with a ref, never a result.
func (caller listing) notServedYet(
	context.Context, *sdk.CallToolRequest,
) (*sdk.CallToolResult, error) {
	_, refused := api.Refusal(caller.request,
		errors.New("mcp: tools/call is not served yet"))
	encoded, err := json.Marshal(map[string]api.Error{"error": refused})
	if err != nil {
		return nil, err
	}
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: string(encoded)}},
	}, nil
}
