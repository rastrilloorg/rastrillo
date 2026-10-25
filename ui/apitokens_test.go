package ui

import (
	"html/template"
	"strings"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
)

var (
	tokensDay   = time.Date(2026, 8, 2, 9, 12, 0, 0, time.UTC)
	tokensUsed  = time.Date(2026, 8, 11, 17, 40, 0, 0, time.UTC)
	tokensUntil = time.Date(2026, 10, 31, 9, 12, 0, 0, time.UTC)
)

// tokensPage is the page with four tokens: a live agent token, a live
// token of the person's own that never expires, a revoked one and an
// expired one; and Claude as the one agent with a live token.
func tokensPage() apitoken.PageData {
	return apitoken.PageData{
		Tokens: []apitoken.Row{
			{Token: apitoken.Token{ID: "k3Jx9QpL2mWa", Subject: "7", Agent: "Claude", Access: rastrillo.Write, CreatedAt: tokensDay, LastUsed: &tokensUsed, ExpiresAt: &tokensUntil}, Display: "Claude for Paul"},
			{Token: apitoken.Token{ID: "Qm2vR8sT4xYb", Subject: "7", Access: rastrillo.Read, CreatedAt: tokensDay}, Display: "Paul"},
			{Token: apitoken.Token{ID: "Zp7wN1cH6kLd", Subject: "7", Agent: "Codex", Access: rastrillo.Write, CreatedAt: tokensDay, RevokedAt: &tokensUsed}, Display: "Codex for Paul", Revoked: true},
			{Token: apitoken.Token{ID: "Vb4tG9yK3nMe", Subject: "7", Agent: "Nightly import", Access: rastrillo.Read, CreatedAt: tokensDay, ExpiresAt: &tokensUsed}, Display: "Nightly import for Paul", Expired: true},
		},
		Agents: []string{"Claude"},
		Form:   map[string]string{"agent": "", "for_self": "", "access": "write", "expiry": "90"},
		CSRFOK: true,
	}
}

// tokensCreatedPage is the page in the response that minted the first
// token: the one state that renders the secret, so the fixture every
// stylesheet check renders carries it.
func tokensCreatedPage() apitoken.PageData {
	p := tokensPage()
	p.Created = &apitoken.Created{Secret: "notes_k3Jx9QpL2mWa_SECRETSECRETSECRET", Token: p.Tokens[0]}
	return p
}

func tokensData(p apitoken.PageData) map[string]any {
	return map[string]any{"Page": p, "Path": "/tokens"}
}

// The list: each row names its actor through the actor partial with
// the handlers' own words, shows the public ID as code, writes its
// dates as ISO days in <time>, and ends in a kebab whose one item
// posts the revoke, or in the word for its state instead.
func TestAPITokensListsEveryToken(t *testing.T) {
	got := render(t, "api-tokens", tokensData(tokensPage()))
	for _, want := range []string{
		`<div rst-tokens>`, `<div rst-card rst-tokens-list>`, `<div rst-lrow="head">`,
		`<span rst-actor>Claude for Paul</span>`, `<code>k3Jx9QpL2mWa</code>`,
		`<span rst-actor>Paul</span>`,
		`<time datetime="2026-08-02">2026-08-02</time>`,
		`<time datetime="2026-08-11">2026-08-11</time>`,
		`<time datetime="2026-10-31">2026-10-31</time>`,
		defaultTf("rastrillo.ui.api_tokens_access_write"), defaultTf("rastrillo.ui.api_tokens_access_read"),
		defaultTf("rastrillo.ui.api_tokens_expiry_never"),
		`<span rst-tokens-state><span rst-status rst-tone="neutral">` + defaultTf("rastrillo.ui.api_tokens_revoked") + `</span></span>`,
		`<span rst-tokens-state><span rst-status rst-tone="warning">` + defaultTf("rastrillo.ui.api_tokens_expired") + `</span></span>`,
		// Each fact carries its heading as a label, for the phone layout.
		`<span rst-tokens-fact><span rst-tokens-fact-label>` + defaultTf("rastrillo.ui.api_tokens_access") + `</span>` + defaultTf("rastrillo.ui.api_tokens_access_write") + `</span>`,
		`<span rst-tokens-fact><span rst-tokens-fact-label>` + defaultTf("rastrillo.ui.api_tokens_col_created") + `</span><time datetime="2026-08-02">2026-08-02</time></span>`,
		`<span rst-tokens-fact><span rst-tokens-fact-label>` + defaultTf("rastrillo.ui.api_tokens_col_last_used") + `</span><time datetime="2026-08-11">2026-08-11</time></span>`,
		`<span rst-tokens-fact><span rst-tokens-fact-label>` + defaultTf("rastrillo.ui.api_tokens_expires") + `</span>` + defaultTf("rastrillo.ui.api_tokens_expiry_never") + `</span>`,
		`<form method="post" action="/tokens/revoke"><input type="hidden" name="id" value="k3Jx9QpL2mWa"><button type="submit">` + defaultTf("rastrillo.ui.api_tokens_revoke") + `</button></form>`,
		`<summary aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", "Claude for Paul")) + `">`,
		`<form method="post" action="/tokens/revoke-agent"><input type="hidden" name="agent" value="Claude"><button rst-btn type="submit">` + defaultTf("rastrillo.ui.api_tokens_revoke_agent") + `: Claude</button></form>`,
		`<form method="post" action="/tokens/revoke-all"><button rst-btn="danger" type="submit">` + defaultTf("rastrillo.ui.api_tokens_revoke_all") + `</button></form>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// A revoked or expired token has no revoke: two kebabs for the two
	// live tokens, and no revoke form for the other two.
	if n := strings.Count(got, `<details rst-row-menu`); n != 2 {
		t.Errorf("%d kebabs, want 2 (one per live token):\n%s", n, got)
	}
	for _, id := range []string{"Zp7wN1cH6kLd", "Vb4tG9yK3nMe"} {
		if strings.Contains(got, `value="`+id+`"`) {
			t.Errorf("the dead token %s still has a revoke form:\n%s", id, got)
		}
	}
	if strings.Contains(got, template.HTMLEscapeString(defaultTf("rastrillo.ui.api_tokens_reveal"))) || strings.Contains(got, "rst-tokens-secret") {
		t.Errorf("a page with nothing created shows the reveal:\n%s", got)
	}
	if strings.Contains(got, defaultTf("rastrillo.ui.api_tokens_empty")) {
		t.Errorf("a page with tokens says there are none:\n%s", got)
	}
}

// The reveal: the whole token as code, in a callout focus lands on, and
// only when Created is set. The secret is never in an attribute, a
// link or a form value, which is how it would get into a URL.
func TestAPITokensRevealsTheSecretOnce(t *testing.T) {
	p := tokensPage()
	p.Created = &apitoken.Created{Secret: "notes_k3Jx9QpL2mWa_SECRETSECRETSECRET", Token: p.Tokens[0]}
	got := render(t, "api-tokens", tokensData(p))
	want := `<div rst-callout rst-tone="positive" tabindex="-1" autofocus>`
	if !strings.Contains(got, want) {
		t.Errorf("missing %q in\n%s", want, got)
	}
	if !strings.Contains(got, `<strong>`+template.HTMLEscapeString(defaultTf("rastrillo.ui.api_tokens_reveal"))+`</strong>`) {
		t.Errorf("the reveal's sentence is missing:\n%s", got)
	}
	if !strings.Contains(got, `<code rst-tokens-secret>notes_k3Jx9QpL2mWa_SECRETSECRETSECRET</code>`) {
		t.Errorf("the secret is not shown as code:\n%s", got)
	}
	if n := strings.Count(got, "SECRETSECRETSECRET"); n != 1 {
		t.Errorf("the secret appears %d times, want once, as text:\n%s", n, got)
	}
}

// The form echoes what was posted and shows the one error under the
// agent field, wired to the input: aria-invalid and the error id in
// aria-describedby, which is what field does for every form.
func TestAPITokensEchoesTheFormAndItsError(t *testing.T) {
	p := tokensPage()
	p.Form = map[string]string{"agent": "", "for_self": "", "access": "read", "expiry": "365"}
	p.Errors = map[string]string{"agent": defaultTf("rastrillo.ui.api_tokens_error_agent")}
	got := render(t, "api-tokens", tokensData(p))
	for _, want := range []string{
		`<form rst-form method="post" action="/tokens">`,
		`id="agent" name="agent"`, `aria-invalid="true" aria-describedby="agent-error"`,
		`<p rst-field-error id="agent-error" role="alert">` + defaultTf("rastrillo.ui.api_tokens_error_agent") + `</p>`,
		`<input type="checkbox" name="for_self" value="on">`,
		`<input type="radio" name="access" value="write">`, `<input type="radio" name="access" value="read" checked>`,
		`<option value="365" selected>`,
		`<button rst-btn="primary lg" type="submit">` + defaultTf("rastrillo.ui.api_tokens_create") + `</button>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// The fresh form: write and 90 preselected, the switch off, no error.
	fresh := render(t, "api-tokens", tokensData(tokensPage()))
	for _, want := range []string{`<input type="radio" name="access" value="write" checked>`, `<option value="90" selected>`} {
		if !strings.Contains(fresh, want) {
			t.Errorf("fresh form is missing %q in\n%s", want, fresh)
		}
	}
	if strings.Contains(fresh, "agent-error") || strings.Contains(fresh, `name="for_self" value="on" checked`) {
		t.Errorf("fresh form carries an error or a ticked switch:\n%s", fresh)
	}
	ticked := tokensPage()
	ticked.Form["for_self"] = "on"
	if got := render(t, "api-tokens", tokensData(ticked)); !strings.Contains(got, `name="for_self" value="on" checked`) {
		t.Errorf("for_self=on did not tick the switch:\n%s", got)
	}
}

// No tokens: the empty state in place of the list, and no revoke-all
// button for nothing.
func TestAPITokensEmpty(t *testing.T) {
	p := tokensPage()
	p.Tokens, p.Agents = nil, nil
	got := render(t, "api-tokens", tokensData(p))
	if !strings.Contains(got, `<p rst-empty-body>`+defaultTf("rastrillo.ui.api_tokens_empty")+`</p>`) {
		t.Errorf("no empty state:\n%s", got)
	}
	for _, bad := range []string{"rst-tokens-list", "/tokens/revoke-all", "/tokens/revoke-agent"} {
		if strings.Contains(got, bad) {
			t.Errorf("an empty page still renders %q:\n%s", bad, got)
		}
	}
}

// The revoke forms post under Path unless the caller names the routes.
func TestAPITokensRevokePathsFollowPath(t *testing.T) {
	data := tokensData(tokensPage())
	data["Path"] = "/account/tokens"
	got := render(t, "api-tokens", data)
	for _, want := range []string{`action="/account/tokens"`, `action="/account/tokens/revoke"`, `action="/account/tokens/revoke-agent"`, `action="/account/tokens/revoke-all"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	data["RevokePath"], data["RevokeAgentPath"], data["RevokeAllPath"] = "/t/one", "/t/agent", "/t/all"
	got = render(t, "api-tokens", data)
	for _, want := range []string{`action="/account/tokens"`, `action="/t/one"`, `action="/t/agent"`, `action="/t/all"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "/account/tokens/revoke") {
		t.Errorf("a named route still falls back to Path:\n%s", got)
	}
}

// Every attribute the partial emits itself has a rule in tokens.css.
func TestAPITokensAttributesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, name := range []string{"rst-tokens", "rst-tokens-list", "rst-tokens-fact", "rst-tokens-fact-label", "rst-tokens-state", "rst-tokens-secret", "rst-tokens-revoke"} {
		if !tokensStyle(css, name) {
			t.Errorf("tokens.css has no selector for %q", name)
		}
	}
}
