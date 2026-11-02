package notes

import (
	"log/slog"
	"net/http"

	"notes/gen"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/mcp"
	"amadan.net/rastrillo/rastrillo/op"
)

// apiDoors builds the two doors an agent can use instead of the pages,
// both behind a bearer token: the JSON API tree, with the same generated
// operations the bookmarks pages call, and the same operations as MCP
// tools. The MCP config takes its gate, context factory and policy from
// the one api.Config, so none of the three can differ between them.
// Redact keeps a token quoted in an error out of the log.
func apiDoors(
	tokens *apitoken.Store,
	ctxFactory func(*http.Request) *rastrillo.Ctx,
	logger *slog.Logger,
) (tree, tools http.Handler, err error) {
	config := api.Config{
		Verify:    tokens,
		Ctx:       ctxFactory,
		Authorize: op.DefaultPolicy,
		Logger:    logger,
		Redact:    tokens.Redact,
	}
	tree, err = api.New(config, gen.APIRoutes()...)
	if err != nil {
		return nil, nil, err
	}
	tools, err = mcp.New(mcp.Config{
		Gate:      config.Gate(),
		Ctx:       config.Ctx,
		Authorize: config.Authorize,
		Name:      "notes",
		Version:   rastrillo.BuildVersion,
	}, gen.ExposedOperations()...)
	if err != nil {
		return nil, nil, err
	}
	return tree, tools, nil
}
