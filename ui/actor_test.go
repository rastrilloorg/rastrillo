package ui

import (
	"strings"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

func actorData(a rastrillo.Actor) map[string]any {
	return map[string]any{"Actor": a}
}

// A person alone is their name in a plain span: no agent marker, no
// badge, nothing but the words.
func TestActorRendersAPersonAlone(t *testing.T) {
	got := render(t, "actor", actorData(rastrillo.Actor{Subject: "7"}))
	if want := `<span rst-actor>7</span>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// An agent acting for a person is the catalog's sentence, in the same
// plain span: no marker, no badge, as the spec rules.
func TestActorWritesTheAgentSentence(t *testing.T) {
	got := render(t, "actor", actorData(rastrillo.Actor{Subject: "7", Agent: "Claude"}))
	if want := `<span rst-actor>` + defaultTf("rastrillo.ui.agent_for", "agent", "Claude", "person", "7") + `</span>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The app itself, the zero Actor, renders nothing at all: not an empty
// span, which would be a cell with an invisible value in it.
func TestActorRendersNothingForTheApp(t *testing.T) {
	if got := render(t, "actor", actorData(rastrillo.Actor{})); got != "" {
		t.Errorf("the app rendered %q, want nothing", got)
	}
}

// Display is the caller's words, built in Go with rastrillo.ActorPhrase
// when the name lookup needed the request (apitoken's Row.Display).
// The partial shows them as given; the Actor still decides whether
// anything renders at all.
func TestActorShowsTheCallersWords(t *testing.T) {
	got := render(t, "actor", map[string]any{
		"Actor": rastrillo.Actor{Subject: "7", Agent: "Claude"}, "Display": "Claude for Paul",
	})
	if want := `<span rst-actor>Claude for Paul</span>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := render(t, "actor", map[string]any{"Actor": rastrillo.Actor{}, "Display": "Claude for Paul"}); got != "" {
		t.Errorf("the zero Actor rendered %q for Display, want nothing", got)
	}
}

// The names are the caller's own text, escaped like any other value.
func TestActorEscapesTheNames(t *testing.T) {
	got := render(t, "actor", actorData(rastrillo.Actor{Subject: "<b>7</b>", Agent: "Cl&ude"}))
	if strings.Contains(got, "<b>") || !strings.Contains(got, "&lt;b&gt;7&lt;/b&gt;") || !strings.Contains(got, "Cl&amp;ude") {
		t.Errorf("names reached the page unescaped: %q", got)
	}
}

// The one attribute the partial emits has a rule in tokens.css, the
// same drift check every family gets.
func TestActorAttributeIsStyled(t *testing.T) {
	if !tokensStyle(string(TokensCSS()), "rst-actor") {
		t.Error("tokens.css has no selector for \"rst-actor\"")
	}
}
