package action_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"github.com/go-chi/chi/v5"
)

// One Path registers the tag page and fills the redirect to it, so the
// form's action cannot send the person to a URL no route serves.
func ExamplePath() {
	const pagePath action.Path = "/tags/{id}"
	const formPath action.Path = "/tags/{id}/rename"

	router := chi.NewRouter()
	routes := action.NewRoutes(router, func(
		request *http.Request,
	) (*rastrillo.Ctx, error) {
		return &rastrillo.Ctx{}, nil
	})
	routes.Get(pagePath, func(visit *action.Visit) {
		fmt.Fprintf(visit.Response(), "tag %s", visit.PathValue("id"))
	})
	routes.Post(formPath, func(visit *action.Visit) {
		visit.Redirect(pagePath.For(visit.PathValue("id")))
	})

	posted := httptest.NewRecorder()
	router.ServeHTTP(posted,
		httptest.NewRequest(http.MethodPost, formPath.For(7), nil))
	location := posted.Header().Get("Location")
	page := httptest.NewRecorder()
	router.ServeHTTP(page,
		httptest.NewRequest(http.MethodGet, location, nil))
	fmt.Println(posted.Code, location)
	fmt.Println(page.Body.String())
	// Output:
	// 303 /tags/7
	// tag 7
}
