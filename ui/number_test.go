package ui

import (
	"bytes"
	"html/template"
	"math"
	"strings"
	"testing"
)

func TestNumberLocalesAndPrecision(t *testing.T) {
	type count int64
	for _, tc := range []struct {
		locale string
		value  any
		want   string
	}{
		{"", 1169772, "1,169,772"},
		{"en-IE", "1169772", "1,169,772"},
		{"de", 1169772, "1.169.772"},
		{"fr", 1169772, "1\u00a0169\u00a0772"},
		{"hi", 1169772, "11,69,772"},
		{"invalid_locale_!", 1169772, "1,169,772"},
		{"en", count(-1169772), "-1,169,772"},
		{"en", int64(math.MinInt64), "-9,223,372,036,854,775,808"},
		{"en", uint64(math.MaxUint64), "18,446,744,073,709,551,615"},
		{"en", "18446744073709551615", "18,446,744,073,709,551,615"},
		{"de", 1234.56789, "1.234,56789"},
		{"en", float32(1234.5), "1,234.5"},
		{"en", 0.000001, "0.000001"},
		{"en", 0, "0"},
		{"de", "4.1k", "4.1k"},
		{"de", "1,234.50", "1,234.50"},
		{"en", "001234", "001234"},
	} {
		t.Run(tc.locale+"/"+tc.want, func(t *testing.T) {
			got := Funcs(WithLocale(tc.locale))["number"].(func(any) any)(tc.value)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNumberRenderingKeepsMachineValuesAndStrings(t *testing.T) {
	base := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
	base = template.Must(base.Parse(`{{define "test"}}{{template "stat" .Stat}}{{template "detail-list" .Details}}<a href="/items/{{.ID}}">{{.ID}}</a><input value="{{.Count}}">{{number .Unsafe}}{{end}}`))
	for _, locale := range []string{"de", "en", "de"} {
		page := template.Must(base.Clone())
		page.Funcs(Funcs(WithLocale(locale)))
		data := map[string]any{
			"Stat": map[string]any{"Label": "Shares", "Value": "1169772", "Exact": "1169772"},
			"Details": map[string]any{"Items": []map[string]any{
				{"Label": "Count", "Value": 1169772},
				{"Label": "ID", "Value": "1169772"},
			}},
			"ID": "1169772", "Count": 1169772, "Unsafe": "<script>alert(1)</script>",
		}
		var out bytes.Buffer
		if err := page.ExecuteTemplate(&out, "test", data); err != nil {
			t.Fatal(err)
		}
		formatted := "1,169,772"
		if locale == "de" {
			formatted = "1.169.772"
		}
		for _, want := range []string{
			`<data value="1169772">` + formatted + `</data>`,
			`<dd>` + formatted + `</dd>`, `<dd>1169772</dd>`,
			`href="/items/1169772"`, `<input value="1169772">`,
			`&lt;script&gt;alert(1)&lt;/script&gt;`,
		} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("locale %s: missing %s in %s", locale, want, out.String())
			}
		}
	}
}

func TestNumericComponentValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		want string
	}{
		{"badge", map[string]any{"Label": 1169772}, ">1.169.772</span>"},
		{"meter", map[string]any{"Percent": 50, "Text": 1169772}, ">1.169.772</span>"},
		{"list-row-action", map[string]any{"Href": "/1169772", "Main": 1169772, "Sub": 1169772}, `href="/1169772">1.169.772</a><small rst-row-sub>1.169.772</small>`},
		{"pagination", map[string]any{"Items": []map[string]any{{"Label": "1169772", "Href": "?page=1169772"}}}, `href="?page=1169772">1.169.772</a>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := template.Must(template.New("").Funcs(Funcs(WithLocale("de"))).ParseFS(Templates(), "*.html"))
			var out bytes.Buffer
			if err := page.ExecuteTemplate(&out, tc.name, tc.data); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("missing %s in %s", tc.want, out.String())
			}
		})
	}
}
