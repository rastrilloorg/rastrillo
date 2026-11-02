// Package mcp serves an app's operations as MCP tools, over the official
// Go SDK, behind the same gate as the JSON API: the same token check,
// the same policy, and authority checked again while each answer goes
// out. An app hands it descriptors and never imports the SDK itself.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

// Config is the endpoint's wiring. Gate, Ctx and Authorize are required.
type Config struct {
	// Gate is the API tree's token check and pipeline, from
	// api.Config.Gate(), so both doors admit the same callers.
	Gate api.Gate
	// Ctx builds the app's Ctx for a request. MCP sets Actor and
	// Authorize on a copy of what it returns, as the tree does.
	Ctx func(*http.Request) *rastrillo.Ctx
	// Authorize is the app's policy. It is required, as for api.New:
	// name op.DefaultPolicy to keep the default.
	Authorize func(
		ctx context.Context,
		actor rastrillo.Actor,
		operation rastrillo.Operation,
	) error
	// Guard is the app's check of what an answer discloses, run with
	// the fresh actor as it goes out. It needs a Rechecker to run.
	Guard op.ResponseGuard
	// Guarded requires both a Rechecker and a Guard, so an app whose
	// answers need an item-level check cannot start without one.
	Guarded bool
	// Name and Version are the server's in the MCP handshake.
	Name, Version string
	// Instructions is the app's own part of what the server tells an
	// agent on connect; the framework's part comes first.
	Instructions string
	// MaxResult caps one tool result in bytes. Zero or less means 2 MiB.
	MaxResult int
}

const defaultMaxResult = 2 << 20

// envelope is room for the JSON-RPC response around a tool result.
const envelope = 64 << 10

// server is what New checked once, shared by every request.
type server struct {
	config       Config
	gate         api.Gate
	tools        []*sdk.Tool
	descriptors  map[string]op.Descriptor
	instructions string
}

// New builds the MCP endpoint over descriptors, one tool each. It checks
// everything here, at boot, so a descriptor that would answer wrongly
// fails the app's start instead of an agent's call.
//
// Each request gets its own stateless server, answered as plain JSON.
// tools/list shows a caller only the tools Admit and op.Check allow
// them, and that list is checked again as it goes out. A GET, a batch
// and subscriptions/listen are refused before the SDK sees them.
func New(config Config, descriptors ...op.Descriptor) (http.Handler, error) {
	if err := config.check(); err != nil {
		return nil, err
	}
	tools, byName, err := toolsFor(descriptors)
	if err != nil {
		return nil, err
	}
	if config.MaxResult <= 0 {
		config.MaxResult = defaultMaxResult
	}
	served := &server{
		config:       config,
		gate:         config.Gate,
		tools:        tools,
		descriptors:  byName,
		instructions: instructionsText,
	}
	if config.Instructions != "" {
		served.instructions += "\n\n" + config.Instructions
	}
	served.gate.MaxResponse = maxResponse(
		served.gate.MaxResponse, config.MaxResult)
	// Built once here, so a tool the SDK would panic on registering
	// fails the app's start rather than every request.
	if _, err := served.build(listing{served: served}); err != nil {
		return nil, err
	}
	return served.gate.Wrap(refusingStreams(served)), nil
}

// gateMaxResponse is what the gate means by a MaxResponse of zero.
const gateMaxResponse = 2 << 20

// maxResponse is the gate's cap, raised when a result of maxResult bytes
// would not fit. The result goes out twice, as structuredContent and as
// the text block, and escaping the text can at most double it.
func maxResponse(gateMax int64, maxResult int) int64 {
	if gateMax <= 0 {
		gateMax = gateMaxResponse
	}
	return max(gateMax, 3*int64(maxResult)+envelope)
}

// check refuses wiring that would fail on the first request, or that
// would look like protection and give none.
func (config Config) check() error {
	switch {
	case config.Ctx == nil:
		return errors.New("mcp: Config.Ctx is required")
	case config.Authorize == nil:
		return errors.New("mcp: Config.Authorize is required; " +
			"name op.DefaultPolicy to keep the default")
	}
	if err := config.Gate.Validate(); err != nil {
		return err
	}
	_, rechecks := config.Gate.Verify.(api.Rechecker)
	switch {
	case config.Guarded && config.Guard == nil:
		return errors.New("mcp: Config.Guarded needs a Config.Guard")
	case config.Guarded && !rechecks:
		return errors.New("mcp: Config.Guarded needs a Gate.Verify " +
			"that is an api.Rechecker")
	case config.Guard != nil && !rechecks:
		return errors.New("mcp: Config.Guard runs only when Gate.Verify " +
			"is an api.Rechecker; use a Verifier with Recheck")
	}
	return nil
}

func (served *server) ServeHTTP(
	response http.ResponseWriter, request *http.Request,
) {
	appContext := served.config.Ctx(request)
	if appContext == nil {
		api.Refuse(response, request,
			errors.New("mcp: Config.Ctx returned nil"))
		return
	}
	// A copy, because a factory may hand every request the same Ctx,
	// and this request's actor must not show in the next one's.
	caller := listing{served: served, request: request,
		appContext: *appContext}
	// The gate stamped it; After can only have replaced it.
	caller.appContext.Actor, _ = rastrillo.ActorFromContext(
		request.Context())
	caller.appContext.Authorize = served.config.Authorize
	built, err := served.build(caller)
	if err != nil {
		api.Refuse(response, request, err)
		return
	}
	handler := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return built },
		&sdk.StreamableHTTPOptions{
			Stateless: true, JSONResponse: true,
			// The SDK's DNS-rebinding check would refuse every app behind
			// a loopback proxy, and the gate has already required a bearer
			// token, which a rebinding page does not have.
			DisableLocalhostProtection: true,
		})
	handler.ServeHTTP(response, request)
}

// build is one request's server. Every tool is registered, so a call to
// one the list hides still reaches Admit and op.Check and is refused as
// the API would, rather than as an unknown tool.
func (served *server) build(caller listing) (*sdk.Server, error) {
	built := sdk.NewServer(
		&sdk.Implementation{
			Name: served.config.Name, Version: served.config.Version,
		},
		&sdk.ServerOptions{
			Instructions: served.instructions,
			// Explicit, or the SDK advertises list changes as soon as a
			// tool is registered, over a stream this endpoint refuses.
			Capabilities: &sdk.ServerCapabilities{
				Tools: &sdk.ToolCapabilities{ListChanged: false},
			},
			SetCacheable: private,
		})
	for _, tool := range served.tools {
		if err := served.addTool(built, tool, caller.notServedYet); err != nil {
			return nil, err
		}
	}
	built.AddReceivingMiddleware(caller.narrowing)
	return built, nil
}

// addTool uses the low-level AddTool: the generic one validates
// arguments against the schema first, and the API door's own rules
// decide. It panics on what it refuses, such as a bad x-mcp-header.
func (served *server) addTool(
	built *sdk.Server, tool *sdk.Tool, handler sdk.ToolHandler,
) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("mcp: operation %s: the SDK refuses its "+
				"tool: %v", served.descriptors[tool.Name].ID, value)
		}
	}()
	built.AddTool(tool, handler)
	return nil
}

// private marks every cacheable result as this caller's alone, since
// the list differs from one token to the next.
func private(_ context.Context, _ sdk.Request, cacheable *sdk.Cacheable) {
	cacheable.CacheScope = "private"
}
