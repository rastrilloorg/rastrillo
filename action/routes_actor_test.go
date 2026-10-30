package action_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/sessions"
	"github.com/go-chi/chi/v5"
)

type actorCase struct {
	name    string
	built   rastrillo.Actor
	request func(*http.Request) *http.Request
	want    rastrillo.Actor
}

func actorCases() []actorCase {
	factory := rastrillo.Actor{Subject: "factory", Access: rastrillo.Read}
	fromContext := rastrillo.Actor{Subject: "ana", Access: rastrillo.Write}
	fromSession := rastrillo.Actor{Subject: "sam", Access: rastrillo.Write}
	session := sessions.Session{Subject: "sam"}

	return []actorCase{
		{"the request's actor wins over both", factory,
			func(request *http.Request) *http.Request {
				request = rastrillo.WithActor(request, fromContext)
				return sessions.WithSession(request, session)
			}, fromContext},
		{"the factory's actor is kept over a session", factory,
			func(request *http.Request) *http.Request {
				return sessions.WithSession(request, session)
			}, factory},
		{"the factory's actor is kept when nothing else is there", factory,
			func(request *http.Request) *http.Request {
				return request
			}, factory},
		{"the session fills in when both are absent", rastrillo.Actor{},
			func(request *http.Request) *http.Request {
				return sessions.WithSession(request, session)
			}, fromSession},
		{"an anonymous request stays anonymous", rastrillo.Actor{},
			func(request *http.Request) *http.Request {
				return request
			}, rastrillo.Actor{}},
	}
}

func TestTheActorAHandlerSeesFollowsTheRulesOfPrecedence(
	t *testing.T,
) {
	for _, scenario := range actorCases() {
		shared := &rastrillo.Ctx{Actor: scenario.built}
		var seen rastrillo.Actor
		router := chi.NewRouter()
		routes := action.NewRoutes(router, func(
			request *http.Request,
		) (*rastrillo.Ctx, error) {
			return shared, nil
		})
		routes.Get("/who", func(visit *action.Visit) {
			seen = visit.Ctx().Actor
		})
		request := scenario.request(
			httptest.NewRequest(http.MethodGet, "/who", nil),
		)
		router.ServeHTTP(httptest.NewRecorder(), request)
		if seen != scenario.want {
			t.Errorf("%s: handler saw %+v, want %+v",
				scenario.name, seen, scenario.want)
		}
		if shared.Actor != scenario.built {
			t.Errorf("%s: the shared Ctx's Actor became %+v, want %+v",
				scenario.name, shared.Actor, scenario.built)
		}
	}
}
