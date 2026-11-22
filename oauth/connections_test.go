package oauth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/oauth"
)

func TestConnectionsListAndEnd(t *testing.T) {
	ctx := context.Background()
	tf := newTokenFlow(t)
	first := tf.connect("write")
	tf.now = tf.now.Add(time.Minute)
	second := tf.connect("read")

	cs, err := tf.srv.Connections(ctx, "ana")
	if err != nil || len(cs) != 2 || cs[0].Access != rastrillo.Read ||
		cs[1].Access != rastrillo.Write {
		t.Fatalf("Connections = %+v, %v; want newest first", cs, err)
	}
	if others, err := tf.srv.Connections(ctx, "bob"); err != nil ||
		len(others) != 0 {
		t.Fatalf("bob's connections %+v, %v", others, err)
	}

	if err := tf.srv.End(ctx, "ana", cs[1].ID); err != nil {
		t.Fatal(err)
	}
	tf.dead(first.AccessToken)
	tf.refusedWith(tf.token(refreshForm(first.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
	tf.works(second.AccessToken)
	left, err := tf.srv.Connections(ctx, "ana")
	if err != nil || len(left) != 1 || left[0].ID != cs[0].ID {
		t.Fatalf("after End: %+v, %v", left, err)
	}
	if err := tf.srv.End(ctx, "ana", cs[1].ID); err != nil {
		t.Errorf("ending twice: %v", err)
	}
}

func TestEndOthersRefused(t *testing.T) {
	ctx := context.Background()
	tf := newTokenFlow(t)
	tokens := tf.connect("write")
	id := tf.connections()[0].ID
	for _, tt := range []struct{ subject, id string }{
		{"bob", id}, {"ana", "missing"}, {"", id},
	} {
		err := tf.srv.End(ctx, tt.subject, tt.id)
		if !errors.Is(err, oauth.ErrNotFound) {
			t.Errorf("End(%q, %q) = %v, want ErrNotFound",
				tt.subject, tt.id, err)
		}
	}
	tf.works(tokens.AccessToken)
	tf.issued(tf.token(refreshForm(tokens.RefreshToken)))
}
