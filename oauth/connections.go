package oauth

import "context"

// Connections lists subject's live connections, newest first, for the
// app to show beside its tokens.
func (server *Server) Connections(
	ctx context.Context, subject string,
) ([]Connection, error) {
	return server.cfg.Store.ConnectionsOf(ctx, subject, server.cfg.Now())
}

// End ends one of subject's connections and its tokens. Another
// person's connection, or an unknown one, is ErrNotFound.
func (server *Server) End(ctx context.Context, subject, id string) error {
	connection, err := server.cfg.Store.Connection(ctx, id)
	if err != nil {
		return err
	}
	if connection.Subject != subject {
		return ErrNotFound
	}
	return server.end(ctx, id)
}

// end ends the connection before its tokens, so a failure between the
// two leaves no token working: Grant refuses an ended connection's.
func (server *Server) end(ctx context.Context, id string) error {
	err := server.cfg.Store.EndConnection(ctx, id, server.cfg.Now())
	if err != nil {
		return err
	}
	return server.cfg.Tokens.RevokeConnection(ctx, id)
}
