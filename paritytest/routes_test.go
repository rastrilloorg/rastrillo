package paritytest

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/route"
)

// spy stands in for the test given to Routes and keeps every failure it
// reports, so a test can hold Routes to the exact messages.
type spy struct {
	testing.TB
	failures []string
}

func (recorder *spy) Errorf(format string, arguments ...any) {
	recorder.failures = append(recorder.failures,
		fmt.Sprintf(format, arguments...))
}

func (recorder *spy) Error(arguments ...any) {
	recorder.failures = append(recorder.failures, fmt.Sprint(arguments...))
}

// wantFailures runs Routes over surface and compares what it reported
// with want, in any order.
func wantFailures(t *testing.T, surface Surface, want ...string) {
	t.Helper()
	recorder := &spy{TB: t}
	Routes(recorder, surface)
	got := slices.Sorted(slices.Values(recorder.failures))
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		t.Errorf("Routes reported:\n\t%s\nwant:\n\t%s",
			strings.Join(got, "\n\t"), strings.Join(want, "\n\t"))
	}
}

// unrecordedMount is what Routes reports for a handler mounted at
// pattern on a chi router: one failure for each method chi serves a
// mount at, read from chi itself so a chi release can't break the test.
func unrecordedMount(pattern string) []string {
	router := chi.NewRouter()
	router.Mount(strings.TrimSuffix(pattern, "*"), http.NotFoundHandler())
	var failures []string
	_ = chi.Walk(router, func(method, path string, _ http.Handler,
		_ ...func(http.Handler) http.Handler) error {
		failures = append(failures, fmt.Sprintf(msgUnrecorded, method, path))
		return nil
	})
	return failures
}

func TestParityFailsARouteWithNoMark(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router, exampleOptions{
			extraMark: func(*exampleApp) route.Mark { return route.Mark{} },
		})
		wantFailures(t, surface, fmt.Sprintf(msgUnmarked, "GET", "/extra"))
	})
}

func TestParityFailsARouteNamingAnUnlistedOperation(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router, exampleOptions{
			extraMark: func(app *exampleApp) route.Mark {
				return route.Operation(app.export.Descriptor)
			},
		})
		wantFailures(t, surface,
			fmt.Sprintf(msgUndeclared, "GET", "/extra", "notes.export"))
	})
}

func TestParityFailsAnOperationWithNoPage(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router,
			exampleOptions{pageLeavesOut: "notes.delete"})
		wantFailures(t, surface, fmt.Sprintf(msgNoPage, "notes.delete"))
	})
}

func TestParityFailsAnOperationMissingFromTheAPI(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router,
			exampleOptions{apiLeavesOut: "notes.delete"})
		wantFailures(t, surface,
			fmt.Sprintf(msgMissing, "notes.delete", "the API",
				"api.Operations"),
			fmt.Sprintf(msgNotMounted, "POST", "/api/v1/notes/delete",
				http.StatusNotFound, "", "notes.delete"))
	})
}

func TestParityFailsAnOperationMissingFromMCP(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router,
			exampleOptions{mcpLeavesOut: "notes.delete"})
		wantFailures(t, surface,
			fmt.Sprintf(msgMissing, "notes.delete", "MCP", "mcp.New"),
			fmt.Sprintf(msgNotListed, "notes.delete"))
	})
}

func TestParityFailsAnAgentOnlyOperationOnEitherDoor(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router,
			exampleOptions{apiExtra: true})
		wantFailures(t, surface,
			fmt.Sprintf(msgUnexpected, "the API", "notes.export"))
		surface, _ = buildExample(t, router,
			exampleOptions{mcpExtra: true})
		wantFailures(t, surface,
			fmt.Sprintf(msgUnexpected, "MCP", "notes.export"))
	})
}

func TestParityFailsTheWrongOperationAtAPath(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router,
			exampleOptions{wrongOperation: true})
		wantFailures(t, surface,
			fmt.Sprintf(msgWrongOperation, "notes.create", "POST",
				"/api/v1/notes/delete", "notes.delete"),
			fmt.Sprintf(msgNotMounted, "POST", "/api/v1/notes/delete",
				http.StatusUnauthorized, "notes.create", "notes.delete"))
	})
}

func TestParityFailsAnAPIBehindACatchAll(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router,
			exampleOptions{catchAll: true})
		want := []string{fmt.Sprintf(msgHumanCallable, "tokens.create",
			"the API", "unauthorized")}
		for _, path := range []string{
			"POST /api/v1/notes/create", "GET /api/v1/notes/list",
			"POST /api/v1/notes/delete", "POST /api/v1/tokens/create",
		} {
			method, target, _ := strings.Cut(path, " ")
			id := strings.Replace(
				strings.TrimPrefix(target, "/api/v1/"), "/", ".", 1)
			want = append(want, fmt.Sprintf(msgNotMounted, method, target,
				http.StatusUnauthorized, "", id))
		}
		// On chi, the walk also finds the API mounted outside the table.
		if router == "chi" {
			want = append(want, unrecordedMount("/api/v2/*")...)
		}
		wantFailures(t, surface, want...)
	})
}

func TestParityFailsAHumanOnlyOperationAnAgentCanCall(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		options := exampleOptions{humanOnlyCallable: true}
		surface, app := buildExample(t, router, options)
		minted := CallAPI(t, surface, app.tokens.Descriptor,
			map[string]any{"agent": "Claude"})
		if minted.Kind != "ok" || app.mintedCount() != 1 {
			t.Fatalf("the unsafe tokens.create answered %+v and minted "+
				"%d, want ok and 1", minted, app.mintedCount())
		}
		surface, app = buildExample(t, router, options)
		served := declarationOf(app.unsafeTokens())
		declared := declarationOf(app.tokens.Descriptor)
		wantFailures(t, surface,
			fmt.Sprintf(msgDiffers, "the API", "tokens.create",
				served, declared),
			fmt.Sprintf(msgDiffers, "MCP", "tokens.create", served, declared),
			fmt.Sprintf(msgHumanListed, "tokens.create"),
			fmt.Sprintf(msgHumanCallable, "tokens.create", "the API", "ok"),
			fmt.Sprintf(msgHumanCallable, "tokens.create", "MCP", "ok"))
	})
}

func TestParityFailsAHumanOnlyOperationServedAtASecondPath(
	t *testing.T,
) {
	eachRouter(t, func(t *testing.T, router string) {
		options := exampleOptions{humanOnlyElsewhere: true}
		surface, app := buildExample(t, router, options)
		minting := call{descriptor: app.tokens.Descriptor, token: "writer-a",
			arguments: []byte(`{"agent":"Claude"}`), session: agentSession}
		answer := send(t, surface.Handler, http.MethodPost,
			"/api/v1/tokens/mint", mcpHeader(minting.header()),
			minting.arguments)
		if answer.status != http.StatusOK || app.mintedCount() != 1 {
			t.Fatalf("POST /api/v1/tokens/mint answered %d and minted "+
				"%d, want 200 and 1", answer.status, app.mintedCount())
		}
		surface, app = buildExample(t, router, options)
		wantFailures(t, surface, fmt.Sprintf(msgDiffers, "the API",
			"tokens.create", declarationOf(app.unsafeTokens()),
			declarationOf(app.tokens.Descriptor)))
	})
}

func TestParityFailsARouteRegisteredDirectlyUnderTheAPIPrefix(
	t *testing.T,
) {
	surface, _ := buildExample(t, "chi",
		exampleOptions{webhookOnRouter: true})
	wantFailures(t, surface,
		fmt.Sprintf(msgUnrecorded, "POST", "/api/v1/webhooks/stripe"))
}

// A route registered at a door's own path is not the door's mount, so
// the walk still finds it.
func TestParityFailsARouteRegisteredDirectlyAtTheMCPPath(t *testing.T) {
	surface, _ := buildExample(t, "chi", exampleOptions{getOnMCP: true})
	wantFailures(t, surface,
		fmt.Sprintf(msgUnrecorded, "GET", "/api/v1/mcp"))
}

// Generated operations are gen/paritytest's to check, so serving one on
// both doors and naming it from a page is no failure here.
func TestParityLeavesGeneratedOperationsToGenParitytest(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, app := buildExample(t, router, exampleOptions{
			apiExtra: true, mcpExtra: true,
			extraMark: func(app *exampleApp) route.Mark {
				return route.Operation(app.export.Descriptor)
			},
		})
		surface.Generated = []op.Descriptor{app.export.Descriptor}
		wantFailures(t, surface)
	})
}

func TestParityFailsARouteRegisteredOutsideTheTable(t *testing.T) {
	surface, _ := buildExample(t, "chi", exampleOptions{directRoutes: true})
	want := append(unrecordedMount("/files/*"),
		fmt.Sprintf(msgUnrecorded, "GET", "/direct"))
	wantFailures(t, surface, want...)
}

func TestParityReportsEveryProblemInOneRun(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, app := buildExample(t, router, exampleOptions{
			extraMark:     func(*exampleApp) route.Mark { return route.Mark{} },
			pageLeavesOut: "notes.list",
			apiExtra:      true,
			mcpLeavesOut:  "notes.delete",
		})
		surface.Operations = append(surface.Operations,
			app.create.Descriptor)
		wantFailures(t, surface,
			fmt.Sprintf(msgUnmarked, "GET", "/extra"),
			fmt.Sprintf(msgListedTwice, "notes.create"),
			fmt.Sprintf(msgNoPage, "notes.list"),
			fmt.Sprintf(msgUnexpected, "the API", "notes.export"),
			fmt.Sprintf(msgMissing, "notes.delete", "MCP", "mcp.New"),
			fmt.Sprintf(msgNotListed, "notes.delete"))
	})
}
