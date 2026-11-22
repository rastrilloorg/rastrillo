package api

import (
	"errors"
	"fmt"
	"net/http"
)

// Challenge points an unauthenticated client at the app's OAuth
// metadata, as the MCP authorization specification asks of a 401.
type Challenge struct {
	// ResourceMetadata is the URL of the protected resource metadata
	// document for this door.
	ResourceMetadata string
	// Scope is the space-separated scopes the door asks for.
	Scope string
}

// header is the WWW-Authenticate value. Validate has already refused
// any character that would need escaping inside the quotes.
func (challenge Challenge) header() string {
	if challenge == (Challenge{}) {
		return "Bearer"
	}
	return fmt.Sprintf("Bearer resource_metadata=%q, scope=%q",
		challenge.ResourceMetadata, challenge.Scope)
}

// validate refuses a challenge with no metadata URL, which would point
// a client nowhere, and any value a quoted-string cannot carry as
// written: only printable ASCII other than a quote or backslash.
func (challenge Challenge) validate() error {
	if challenge == (Challenge{}) {
		return nil
	}
	if challenge.ResourceMetadata == "" {
		return errors.New("api: Challenge has no ResourceMetadata")
	}
	values := []string{challenge.ResourceMetadata, challenge.Scope}
	for _, value := range values {
		for _, character := range value {
			if character < ' ' || character > '~' ||
				character == '"' || character == '\\' {
				return fmt.Errorf("api: Challenge %q contains a "+
					"character that is not printable ASCII, a quote, "+
					"or a backslash", value)
			}
		}
	}
	return nil
}

type challengeKey struct{}

// challengeFrom is the challenge serve stamped, or the zero one.
func challengeFrom(request *http.Request) Challenge {
	if request == nil {
		return Challenge{}
	}
	challenge, _ := request.Context().Value(challengeKey{}).(Challenge)
	return challenge
}
