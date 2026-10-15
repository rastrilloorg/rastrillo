package ui

import (
	"net/http/httptest"
	"testing"
)

func TestLocalizerFindsLInEveryDataShape(t *testing.T) {
	l := NewLocalizer("es", "es", func(k string, _ ...any) string { return k })
	type embeds struct {
		Localized
		Title string
	}
	type own struct{ L *Localizer }
	var nilStruct *embeds
	for name, c := range map[string]struct {
		data any
		want *Localizer
	}{
		"map":                {map[string]any{"L": l}, l},
		"struct field":       {own{L: l}, l},
		"embedded Localized": {embeds{Localized: Localized{L: l}}, l},
		"pointer to struct":  {&embeds{Localized: Localized{L: l}}, l},
		"nil pointer":        {nilStruct, nil},
		"L is a string":      {map[string]any{"L": "es"}, nil},
		"no L":               {map[string]any{"X": 1}, nil},
		"nil data":           {nil, nil},
		"not a container":    {42, nil},
	} {
		if got := localizer(c.data); got != c.want {
			t.Errorf("%s: localizer = %p, want %p", name, got, c.want)
		}
	}
}

func TestWithLCopiesMapsAndPassesStructsThrough(t *testing.T) {
	l := NewLocalizer("es", "es", func(k string, _ ...any) string { return k })
	orig := map[string]any{"X": 1}
	got, ok := withL(orig, l).(map[string]any)
	if !ok || got["L"] != l || got["X"] != 1 {
		t.Errorf("withL(map) = %#v", got)
	}
	if _, leaked := orig["L"]; leaked {
		t.Error("withL wrote L into the caller's map, which other templates may share")
	}
	type view struct{ Localized }
	got2, ok := withL(view{Localized{L: l}}, nil).(view)
	if !ok || got2.L != l {
		t.Errorf("withL changed a struct to %#v: a struct must carry its own L", got2)
	}
}

func TestLocalizerAccessors(t *testing.T) {
	l := NewLocalizer("pt", "pt-PT", func(k string, _ ...any) string { return k })
	if l.Identity() != "pt" || l.Format() != "pt-PT" {
		t.Errorf("Identity, Format = %q, %q", l.Identity(), l.Format())
	}
	in := Localized{L: l, Return: "/x"}
	if in.Localizer() != l || in.Request().Return != "/x" {
		t.Error("Localized's methods do not return what it holds")
	}
	var view struct{ Localized }
	view.Localized = in
	var r interface{ Request() Localized } = view
	if r.Request().L != l {
		t.Error("Request is not promoted to a struct embedding Localized")
	}
}

func TestLocalizerTravelsInTheRequestContext(t *testing.T) {
	l := NewLocalizer("es", "es", func(k string, _ ...any) string { return k })
	r := httptest.NewRequest("GET", "/es/items?page=2", nil)
	if LocalizerFrom(r) != nil {
		t.Error("LocalizerFrom found a localizer nobody set")
	}
	r = WithLocalizer(r, l)
	if LocalizerFrom(r) != l {
		t.Error("LocalizerFrom(WithLocalizer(r, l)) is not l")
	}
	got := ForRequest(r)
	if got.L != l || got.Return != "/es/items?page=2" || got.Locales != nil {
		t.Errorf("ForRequest = %+v", got)
	}
}
