package rastrillo

import (
	"log/slog"
	"os"

	"amadan.net/rastrillo/rastrillo/carlos"
)

// Origin is the app's external origin, which the CSRF check compares
// with and emailed links start with: the environment variable named
// variable, else https:// and the instance's own host on CARLOS, else
// http://localhost:8080. The default is loud, because a quiet default in
// production is a bug.
//
// The instance's own host is not where people reach the app behind a
// custom domain or under a hub's /<slug>, so those set variable.
func Origin(variable string, logger *slog.Logger) string {
	if origin := os.Getenv(variable); origin != "" {
		return origin
	}
	if host := carlos.Host(); host != "" {
		origin := "https://" + host
		logger.Info("origin from CARLOS_HOST; set "+variable+
			" to serve another host", "origin", origin)
		return origin
	}
	origin := "http://localhost:8080"
	logger.Warn(variable+" not set; defaulting", "origin", origin)
	return origin
}
