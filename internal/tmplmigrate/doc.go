// Package tmplmigrate is the codemod behind `rastrillo migrate-templates`:
// it rewrites templates to the localizer-first syntax from
// text/template's own parse tree, and validates every result by parsing
// it again before anything is written.
//
// The rewrites, each an insertion at a parsed position:
//
//  1. Every call of a locale function (ui.LocaleFuncs) gains $L as its
//     first argument.
//  2. Every {{template "name" dict ...}} gains "L" $L.
//  3. A {{template "name" PIPELINE}} whose data is neither a dict nor the
//     template's own $ has its complete pipeline wrapped:
//     (withL (PIPELINE) $L). The site is listed for review, because
//     withL can add L to a map but not to a struct.
//  4. Every template body that now mentions $L starts with
//     {{$L := localizer $}}.
//
// It refuses a file that already binds $L to anything else, anywhere,
// and reports every {{template "name"}} with no data at all (the callee
// receives nil, so no localizer). It is idempotent.
package tmplmigrate
