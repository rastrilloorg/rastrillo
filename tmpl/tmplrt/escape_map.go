package tmplrt

// Escapers maps the names html/template gives its escaper functions to
// Rastrillo's ports of them. Each value is a func(args ...any) string.
// It is the same set as html/template's funcMap, _eval_args_ included,
// so a template.FuncMap built from it can stand in for html/template's
// own: an escaped tree calls nothing else.
var Escapers = map[string]any{
	"_html_template_attrescaper":      AttrEscaper,
	"_html_template_commentescaper":   CommentEscaper,
	"_html_template_cssescaper":       CSSEscaper,
	"_html_template_cssvaluefilter":   CSSValueFilter,
	"_html_template_htmlnamefilter":   HTMLNameFilter,
	"_html_template_htmlescaper":      HTMLEscaper,
	"_html_template_jsregexpescaper":  JSRegexpEscaper,
	"_html_template_jsstrescaper":     JSStrEscaper,
	"_html_template_jstmpllitescaper": JSTmplLitEscaper,
	"_html_template_jsvalescaper":     JSValEscaper,
	"_html_template_nospaceescaper":   HTMLNospaceEscaper,
	"_html_template_rcdataescaper":    RCDATAEscaper,
	"_html_template_srcsetescaper":    SrcsetEscaper,
	"_html_template_urlescaper":       URLEscaper,
	"_html_template_urlfilter":        URLFilter,
	"_html_template_urlnormalizer":    URLNormalizer,
	"_eval_args_":                     EvalArgs,
}
