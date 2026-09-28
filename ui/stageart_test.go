package ui

import (
	"html/template"
	"regexp"
	"strings"
	"testing"
)

func TestStageArtIsDeterministicPerSeed(t *testing.T) {
	if stageArt("fichas") != stageArt("fichas") {
		t.Fatal("one seed drew two pictures; an app's backdrop must not change between loads")
	}
	if stageArt("fichas") == stageArt("harbour") {
		t.Fatal("two seeds drew the same picture; two apps should differ")
	}
}

// The rendered output, not the template source: the inline-style test
// reads partials and layouts as source, and would never see what this
// function generates. fill=, stroke= and color= are refused as well as
// literals: a presentation attribute is a colour the stylesheet does not
// own, whatever value it carries, and the theme and scheme would stop
// reaching the art.
func TestStageArtIsSmallDecorativeAndPaintedOnlyByCSS(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)style|#[0-9a-f]{3,8}\b|rgba?\(|hsla?\(|url\(|href|https?:|<script|<image|<use|\b(fill|stroke|color)=`)
	for i := 0; i < 200; i++ {
		seed := "seed-" + strings.Repeat("x", i%7) + string(rune('a'+i%26))
		out := string(stageArt(seed))
		if len(out) >= 4096 {
			t.Errorf("seed %q: %d bytes, want under 4 KB", seed, len(out))
		}
		if m := forbidden.FindString(out); m != "" {
			t.Errorf("seed %q: output carries %q; colour comes from tokens.css so the art follows the theme and scheme", seed, m)
		}
		for _, want := range []string{`<svg rst-stage-art aria-hidden="true" focusable="false"`, "<g rst-stage-art-glow>", "<g rst-stage-art-lines "} {
			if !strings.Contains(out, want) {
				t.Errorf("seed %q: missing %s", seed, want)
			}
		}
	}
}

func TestStageShellCarriesTheBackdropSlot(t *testing.T) {
	src, ok := Layout("stage")
	if !ok {
		t.Fatal("no stage shell")
	}
	for _, want := range []string{
		`<body rst-stage>`,
		`<div rst-stage-scene aria-hidden="true">{{block "backdrop" .}}{{stageArt "rastrillo"}}{{end}}</div>`,
		`<main rst-page id="main">`,
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("layouts/stage.html lacks %s", want)
		}
	}

	// Rendered with nil data, the way a handler with nothing to pass
	// renders it: the default backdrop must arrive as markup, not as
	// escaped text, and must not depend on anything the page carries.
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset": func(p string) string { return "/" + p },
	}).Parse(string(src)))
	template.Must(tmpl.Parse(`{{define "content"}}CONTENT-SENTINEL{{end}}`))
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
		t.Fatal(err)
	}
	if want := `<div rst-stage-scene aria-hidden="true">` + string(stageArt("rastrillo")) + `</div>`; !strings.Contains(b.String(), want) {
		t.Errorf("the stage shell does not draw stageArt(\"rastrillo\") behind the card:\n%s", b.String())
	}
}

// Hidden where a picture of waves would do harm: forced colours paint
// every stroke in the system's text colour at full strength, and a
// printout does not need it. The stylesheet is the only place this can
// happen, because the art carries no style of its own.
func TestStageArtIsHiddenUnderForcedColoursAndInPrint(t *testing.T) {
	css := string(TokensCSS())
	for _, media := range []string{"@media (forced-colors: active)", "@media print"} {
		re := regexp.MustCompile(regexp.QuoteMeta(media) + ` \{\s*\.rst-stage-art, \[rst-stage-art\] \{ display: none; \}\s*\}`)
		if !re.MatchString(css) {
			t.Errorf("tokens.css does not hide [rst-stage-art] under %s", media)
		}
	}
}
