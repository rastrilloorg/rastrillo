package app

import (
	"html/template"
	"strings"
	textt "text/template"

	"example.com/gocheck/keys"

	"amadan.net/rastrillo/rastrillo/ui"
)

// A "T" in a comment is not a registration.
var notAFuncMap = map[string]int{"T": 1}

var funcs = template.FuncMap{
	"T":     nil,
	"money": nil,
	"asset": nil,
}

var byConstant = template.FuncMap{
	keyT:                 nil, // a constant: "T"
	keyMoney:             nil, // built from constants: "money"
	keyAsset:             nil, // "asset": fine
	string(keyTf):        nil, // a conversion of a constant: "Tf"
	strings.ToUpper("t"): nil, // a call that looks like a conversion: not a constant, review
}

var byOtherPackage = template.FuncMap{keys.Name: nil} // another package's constant: "T"

func pick(string) string { return "T" }

func keyFor() string { return "number" }

// register is a helper that receives a FuncMap: its writes are checked.
func register(m template.FuncMap) {
	m["Tbdi"] = nil
}

// addAny receives a map[string]any that later flows into Funcs.
func addAny(m map[string]any) {
	m["displayValue"] = nil
	m[keyFor()] = nil // computed: review
}

func wire(t *template.Template, tt *textt.Template) {
	funcs["Tf"] = nil
	funcs[keyNumber] = nil
	funcs[pick("x")] = nil // single-argument call: not a constant, review

	keyAsset := "T"                     // shadows the package constant
	_ = template.FuncMap{keyAsset: nil} // review: a variable, not the constant "asset"

	made := make(template.FuncMap)
	made["dateWords"] = nil
	register(made)

	anyFuncs := map[string]any{"money": nil}
	addAny(anyFuncs)
	t.Funcs(anyFuncs)

	notFuncs := map[string]any{}
	notFuncs[keyFor()] = nil // never reaches Funcs: not reported
	_ = notFuncs
	notAFuncMap[pick("x")] = 2 // an ordinary map: not reported

	tt.Funcs(textt.FuncMap{"T": nil}) // text/template's FuncMap too

	_ = ui.WithT(nil)
	_ = "T"
}
