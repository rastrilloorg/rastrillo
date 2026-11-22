package oauth

import (
	"net/http"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// Page is what the approval page shows; the app's Render draws it.
type Page struct {
	// Kind is "approve" or "error".
	Kind     string
	SiteName string
	// ClientName is the name the client gave itself; ClientDomain is
	// the host of its metadata document, empty for a registered client.
	ClientName   string
	ClientDomain string
	// SelfNamed marks a registered client, whose name nobody vouches for.
	SelfNamed bool
	// Loopback marks a redirect to an app on this computer.
	Loopback bool
	// ReturnHost is the host the approval returns to.
	ReturnHost string
	// Requested is what the client asked for; Pending is the approval's
	// id, carried by the form.
	Requested rastrillo.Access
	Pending   string
	// Error is what went wrong, in words the person can act on.
	Error string
	// ReturnURL, on an error page, takes the person back to the app with
	// the error; the page links to it as "Return to <ReturnHost>". It is
	// the client's own address, so render it with html/template's
	// escaping, never as template.URL.
	ReturnURL string
}

func (server *Server) errorPage(
	response http.ResponseWriter, request *http.Request, status int, msg string,
) {
	server.render(response, request, status,
		Page{Kind: "error", SiteName: server.cfg.SiteName, Error: msg})
}

// render draws page with status, leaving Render free to set headers.
func (server *Server) render(
	response http.ResponseWriter, request *http.Request, status int, page Page,
) {
	sw := &statusWriter{ResponseWriter: response, status: status}
	err := server.cfg.Render(sw, request, page)
	if err != nil {
		server.cfg.Logger.Error("oauth: rendering the approval page",
			"kind", page.Kind, "err", err)
		if !sw.wrote {
			http.Error(response,
				http.StatusText(http.StatusInternalServerError),
				http.StatusInternalServerError)
		}
		return
	}
	sw.WriteHeader(status)
}

// statusWriter sends its status in place of the 200 a Render that
// writes the body without a status would get.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (writer *statusWriter) WriteHeader(code int) {
	if writer.wrote {
		return
	}
	writer.wrote = true
	if code == http.StatusOK {
		code = writer.status
	}
	writer.ResponseWriter.WriteHeader(code)
}

func (writer *statusWriter) Write(body []byte) (int, error) {
	writer.WriteHeader(http.StatusOK)
	return writer.ResponseWriter.Write(body)
}

func (writer *statusWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}
