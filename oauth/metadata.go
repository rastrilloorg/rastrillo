package oauth

import "net/http"

var scopes = []string{"read", "write"}

type resourceMetadata struct {
	Resource      string   `json:"resource"`
	Servers       []string `json:"authorization_servers"`
	Scopes        []string `json:"scopes_supported"`
	BearerMethods []string `json:"bearer_methods_supported"`
}

type serverMetadata struct {
	Issuer           string   `json:"issuer"`
	Authorization    string   `json:"authorization_endpoint"`
	Token            string   `json:"token_endpoint"`
	Registration     string   `json:"registration_endpoint"`
	Revocation       string   `json:"revocation_endpoint"`
	ResponseTypes    []string `json:"response_types_supported"`
	GrantTypes       []string `json:"grant_types_supported"`
	CodeChallenges   []string `json:"code_challenge_methods_supported"`
	TokenAuthMethods []string `json:"token_endpoint_auth_methods_supported"`
	Scopes           []string `json:"scopes_supported"`
	ClientDocs       bool     `json:"client_id_metadata_document_supported"`
	IssParam         bool     `json:"authorization_response_iss_parameter_supported"`
}

func (server *Server) resourceDocument(door string) http.Handler {
	return jsonHandler(resourceMetadata{
		Resource:      server.Issuer() + door,
		Servers:       []string{server.Issuer()},
		Scopes:        scopes,
		BearerMethods: []string{"header"},
	})
}

func (server *Server) serverDocument() http.Handler {
	return jsonHandler(serverMetadata{
		Issuer:           server.Issuer(),
		Authorization:    server.Issuer() + "/oauth/authorize",
		Token:            server.Issuer() + "/oauth/token",
		Registration:     server.Issuer() + "/oauth/register",
		Revocation:       server.Issuer() + "/oauth/revoke",
		ResponseTypes:    []string{"code"},
		GrantTypes:       []string{"authorization_code", "refresh_token"},
		CodeChallenges:   []string{"S256"},
		TokenAuthMethods: []string{"none"},
		Scopes:           scopes,
		ClientDocs:       true,
		IssParam:         true,
	})
}

func jsonHandler(body any) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		writeJSON(response, http.StatusOK, body)
	})
}
