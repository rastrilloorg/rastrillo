//rastrillo:data template texts of the equivalence corpus, kept as written

package corpus

import "slices"

// constructs is a case for each node type and builtin, small enough
// that a variant which makes one construct fail does not hide another.
func constructs() []Case {
	return slices.Concat(
		pipelineOrder(),
		controlConstructs(),
		dollarRangeConstructs(),
		logicConstructs(),
		builtinConstructs(),
		expressionConstructs(),
		[]Case{
			recursion(),
			depth(),
			channels(),
			missingKey("zero"),
			missingKey("error"),
			missingKey("default"),
			missingKeyPrint("zero"),
			missingKeyPrint("error"),
			missingKeyPrint("default"),
			missingKeyMethod("error"),
			missingKeyMethod("default"),
			lazyArgs(),
		},
	)
}

// controlConstructs are variables and the control statements:
// {{if}}, {{with}} and {{range}} chains, and {{break}} and
// {{continue}}.
func controlConstructs() []Case {
	return []Case{
		construct("variables", `{{$t := .Title}}{{$n := "none"}}<p>{{$t}}</p>
{{range .Items}}{{$n = .Name}}{{end}}<i>{{$n}}</i>
<b title="{{$.Title}}">{{$.Count}}</b>
{{with $o := .Owner}}{{$o.Name}}/{{$.Title}}{{end}}
{{$t = "reassigned"}}{{$t}}
{{$c := .Count}}{{if $c}}{{$c := "shadowed"}}{{$c}}{{end}}{{$c}}`),
		construct(
			"if-chain",
			`{{if .Count}}count {{.Count}}{{else if .Title}}title {{.Title}}{{else if not .Items}}no items{{else}}items{{end}}
{{if $a := .Any}}any {{$a}}{{else}}no any {{$a}}{{end}}
<input{{if .Done}} checked{{end}}>`,
		),
		construct(
			"with-chain",
			`{{with .Owner}}<p>{{.Name}}</p>{{else with .Title}}<p>{{.}}</p>{{else}}<p>nobody</p>{{end}}
{{with $x := .Tags}}{{len $x}}{{end}}
{{with .Any}}<b>{{.}}</b>{{else}}{{.Title}}{{end}}`,
		),
		construct(
			"range-break-continue",
			`<ul>{{range $i, $e := .Items}}{{if ge $i 4}}{{break}}{{end}}{{if not $e.Name}}{{continue}}{{end}}<li data-i="{{$i}}">{{$e.Name}}</li>{{else}}<li>empty</li>{{end}}</ul>
{{range .Items}}{{if eq .Size 12}}{{continue}}{{end}}{{.Size}}{{if gt .Size 4}}{{break}}{{end}},{{end}}`,
		),
		// A {{break}} in a range's {{else}} ends that range statement,
		// as text/template's walkRange recovers it there, and the outer
		// range goes on; a {{continue}} there continues the outer one.
		construct(
			"range-else-break",
			`{{range .Items}}[{{range 0}}{{else}}e{{break}}never{{end}}after {{.Name}}]{{end}}|{{range .Items}}[{{range 0}}{{else}}c{{if .Name}}{{continue}}{{end}}empty{{end}}after]{{end}}`,
		),
		// A range body that always ends in a break or continue has no
		// return after it, which would be unreachable.
		construct(
			"range-terminates",
			`{{range .Items}}{{break}}{{end}}|{{range .Items}}<{{.Name}}>{{continue}}{{end}}|{{range .Items}}{{if .Name}}{{break}}{{else}}{{continue}}{{end}}never{{end}}|{{range .Items}}{{with .Name}}{{continue}}{{else}}{{break}}{{end}}{{end}}`,
		),
	}
}

// dollarRangeConstructs are a $ of a template's own, and {{range}}
// over each kind of value it takes, with and without variables.
func dollarRangeConstructs() []Case {
	return []Case{
		// A $ of the template's own is not the dot it started with.
		construct(
			"dollar-shadow",
			`{{$ := "shadowed"}}{{$}}|{{with .Title}}{{$}}{{end}}`,
		),
		construct("dollar-shadow-after", `{{$.Title}}|{{$ := .Count}}{{$}}`),
		construct(
			"dollar-assign",
			`{{$ = .Count}}{{$}}|{{with .Owner}}{{$}}{{end}}|{{$ = "only set"}}`,
		),
		construct(
			"range-map",
			`{{range $k, $v := .Tags}}{{$k}}={{$v}};{{end}}|{{range .Tags}}{{.}};{{end}}`,
		),
		construct(
			"range-int",
			`{{range $x := .Count}}{{$x}},{{end}}|{{range 3}}{{.}}{{end}}`,
		),
		construct(
			"range-else-vars",
			`{{range $e := .Items}}{{else}}[{{$e}}]{{end}}|{{range $i, $e := .Tags}}{{else}}[{{$i}} {{$e}}]{{end}}`,
		),
		construct(
			"range-assign",
			`{{$last := "none"}}{{range $last = .Items}}{{end}}{{$last}}|{{$i := 0}}{{$e := 0}}{{range $i, $e = .Tags}}{{end}}{{$i}} {{$e}}`,
		),
		construct("range-any", `{{range .Any}}[{{.}}]{{else}}none{{end}}`),
		construct(
			"range-two-any",
			`{{range $k, $v := .Any}}[{{$k}}:{{$v}}]{{else}}none{{end}}`,
		),
	}
}

// logicConstructs are {{template}} and {{block}} in every context,
// and the logic builtins.
func logicConstructs() []Case {
	return []Case{
		construct(
			"templates",
			`{{define "row"}}<li title="{{.Name}}">{{.Name}}</li>{{end}}`+
				`{{define "static"}}<hr>{{.}}{{end}}`+
				`{{define "attr"}}{{.Title}} & co{{end}}`+
				`<ul>{{range .Items}}{{template "row" .}}{{end}}</ul>
{{template "static"}}
<a title="{{template "attr" .}}" href="/x?q={{template "attr" .}}">x</a>
<script>var t = "{{template "attr" .}}";</script>
{{block "side" .Owner}}<aside>{{.Name}}</aside>{{end}}
{{template "row" index .Items 0}}`,
		),
		construct(
			"and-or",
			`{{if and .Owner .Owner.Name}}owner {{.Owner.Name}}{{end}}
{{or .Title "untitled"}}|{{and .Count .Title}}|{{or .Count .Ratio .Done}}|{{and .Done .Count .Title}}
{{if or (not .Items) .Done}}x{{end}}
{{.Title | and .Count}}|{{.Title | or .Any}}`,
		),
		construct(
			"not",
			`{{not .Any}}|{{not .Items}}|{{not .Owner}}|{{not 0}}|{{not ""}}`,
		),
		construct(
			"eq",
			`{{eq .Kind "a"}} {{eq .Kind "a" "b" "named<kind>"}} {{eq .Count 3}} {{eq .Count 3 1}} {{eq .Done true}} {{eq .Title .Title}}`,
		),
	}
}

// builtinConstructs are the other builtins, on typed values and on
// values of any type.
func builtinConstructs() []Case {
	return []Case{
		construct("eq-any", `{{eq .Any 7}} {{eq .Any "x" 1.5 true}}`),
		construct("eq-uint", `{{eq .Count 3}}|{{eq 3 .Count}}`),
		construct(
			"compare",
			`{{ne .Count 0}} {{lt .Count 10}} {{le .Ratio 0.5}} {{gt .Title "m"}} {{ge .Count 3}} {{lt .Kind "b"}}`,
		),
		construct("compare-any", `{{lt .Any 10}} {{ne .Any nil}}`),
		construct("len", `{{len .Items}} {{len .Title}} {{len .Tags}}`),
		construct("len-any", `{{len .Any}}`),
		construct(
			"index",
			`{{index .Tags "a"}}|{{index .Tags "missing"}}|{{(index .Items 1).Name}}|{{index .Title 0}}`,
		),
		construct("index-any", `{{index .Any 0}}|{{index .Any "a"}}`),
		construct(
			"slice",
			`{{slice .Title 1 3}}|{{slice .Title}}|{{len (slice .Items 1)}}|{{slice .Title 1}}`,
		),
		construct("slice-any", `{{slice .Any 1 2}}`),
		construct(
			"print",
			`{{print .Title 1 2.5 true nil "s"}}|{{printf "%d-%s-%v" .Count .Title .Kind}}|{{println .Title .Count}}|{{print}}|{{printf "%T %v" .Any .Any}}|{{printf "%x" 255}}`,
		),
		construct("call", `{{call .Fn .Title}}|{{call .Fn "const"}}`),
		construct(
			"escaper-builtins",
			`{{html .Title}}|{{.Title | html}}|{{js .Title}}|{{printf "%s" (html .Title)}}|{{printf "%s" (urlquery .Title "x")}}|{{html .Any}}
<a href="/s?q={{urlquery .Title}}&r={{.Title | urlquery}}">s</a>
<script>var x = "{{js .Title}}";</script>`,
		),
	}
}

// expressionConstructs are methods, chains, constants, pipelines,
// parentheses, trim markers and comments, and dot in every context.
func expressionConstructs() []Case {
	return []Case{
		construct(
			"methods",
			`{{.Greet "hi"}}|{{.Greet .Title}}|{{"piped" | .Greet}}|{{.Scale 2}}|{{.Scale 0.5}}|{{.Join "-" "a" "b" .Title}}|{{.Join ","}}|{{.Is "a"}}|{{.Owner.Initial}}`,
		),
		construct("pointer-method", `{{.Shout "x" 2}}`),
		construct(
			"chains",
			`{{(.Lookup "a").Name}}|{{$o := .Owner}}{{$o.Name}}|{{(.Owner).Name}}|{{(index .Items 0).Size}}`,
		),
		construct(
			"constants",
			`{{3}}|{{-2}}|{{1.5}}|{{1e3}}|{{0x10}}|{{'a'}}|{{"s"}}|{{true}}|{{false}}|{{2i}}|{{07}}|{{1_000}}`,
		),
		construct(
			"pipelines",
			`{{.Title | printf "%q"}}|{{.Count | printf "%03d" | len}}|{{.Title | printf "%s!" | printf "<%s>"}}|{{.Any | print}}`,
		),
		construct(
			"parens",
			`{{printf "%s-%v" (.Title) (len .Items)}}|{{(.Title)}}|{{(print (print .Count))}}`,
		),
		construct(
			"trim-comments",
			`<p>  {{- .Title -}}  </p>{{/* a comment */}} {{- /* another */ -}} <i>{{.Count}}</i>`,
		),
		construct(
			"contexts",
			`<p>{{.Any}}</p><a title="{{.Any}}" href="{{.Any}}" data-x={{.Any}}>x</a><a href="/p/{{.Title}}?q={{.Any}}">p</a>
<textarea>{{.Any}}</textarea><img srcset="{{.Any}}" alt="{{.Title}}">
<script>var a = {{.Any}}; var s = "{{.Any}}"; var t = `+"`a${ {{.Any}} }b`"+`; var r = /{{.Title}}/;</script>
<style>p { color: {{.Any}} } a { background: url("{{.Title}}") }</style>
<div {{.Any}}></div><!-- {{.Title}} -->`,
		),
		construct("dot", `{{.}}|{{with .}}{{.}}{{end}}|<b title="{{.}}">x</b>`),
	}
}
