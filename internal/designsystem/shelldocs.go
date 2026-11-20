package designsystem

// The shell demos' documents and the pieces of them that are the
// design system's own: a shell with an index is two documents, the
// index and a content page, because a phone shows one or the other and
// one document cannot be both.

// oneRow is what the three shells with an index have in common on a
// phone, in the words of Fiesta's gallery, which says it of the same
// layouts, except for the card: these demos are static, with no
// /_theme, so their card has no Appearance row
// (TestTheStaticDemosCarryNoSchemeMenu), and the words say so.
const oneRow = "Other pages have one header row with a back link, app " +
	"name when it fits, and avatar. The avatar opens the account card, " +
	"with language and account links. In a served app, the card also " +
	"includes appearance settings."

// shellBlurbs is what the Shells page says under each shell.
var shellBlurbs = map[string]string{
	"column": "The plain centred page every scaffolded app starts " +
		"on: a skip link, a title, and the content column.",
	"topbar": "Brand, navigation and an account menu across the top, " +
		"with a footer under the page. On a phone, the navigation is " +
		"an index page at /menu. " + oneRow,
	"sidebar": "A navigation rail beside the page. On a phone, the " +
		"rail is an index page. " + oneRow + " No JavaScript needed.",
	"console": "A top bar and side rail, like an admin console. On a " +
		"phone, the rail is an index page. " + oneRow +
		" No JavaScript needed.",
	"stage": "One card in the middle of a full-page backdrop, for a " +
		"screen that stands alone. The sign-in screen is what it is " +
		"for.",
}

// shellMenuHref is the topbar demo's Menu page, its index. The topbar
// is the other way round from the sidebar and console: its home is a
// content page, shells/topbar.html, which the gallery links, and its
// sections are one level up, on the Menu page.
func shellMenuHref(mount, theme, locale string) string {
	return mount + "/" + theme + "/" + locale + "/shells/topbar-menu.html"
}

// shellDoc is one document of a shell demo: its file under shells/ and
// what it executes against.
type shellDoc struct {
	file string
	data shellData
}

// shellDocs is a shell demo's documents. The sidebar's and console's
// are <shell>.html, the index the gallery has always linked, and
// <shell>-page.html, the content page its nav opens, whose up link
// returns to <shell>.html#nav-posts. The topbar's are topbar.html, its
// home, a content page whose up link returns to its row on
// topbar-menu.html, the index. Every other shell is one document.
func shellDocs(data shellData, mount, theme, locale, shell string) []shellDoc {
	home := shellHref(mount, theme, locale, shell)
	index, page := data, data
	index.View, page.View = "index", "page"
	switch shell {
	case "topbar":
		menu := shellMenuHref(mount, theme, locale)
		index.Up, page.Up = menu, menu+"#nav-posts"
		index.PageHref, page.PageHref = home, home
		return []shellDoc{
			{"topbar.html", page}, {"topbar-menu.html", index}}
	case "sidebar", "console":
		index.Up, page.Up = home, home+"#nav-posts"
		index.PageHref = shellPageHref(mount, theme, locale, shell)
		page.PageHref = index.PageHref
		return []shellDoc{
			{shell + ".html", index}, {shell + "-page.html", page}}
	}
	return []shellDoc{{shell + ".html", data}}
}

// shellLocale is a shell demo's locale block: the account card's
// Language row in the shells that have a card, and in column, which
// has none, the dropdown its layout writes in the foot. A card's row
// in column's foot showed a demo of markup no app gets.
const shellLocale = `{{if eq .Name "column"}}` + languageMenu +
	`{{else}}` + languageRow + `{{end}}`

// languageMenu is the locale partial's dropdown, as column's foot has
// it, with links in place of forms (see languageRow).
const languageMenu = `<details rst-dropdown rst-locale name="rst-menus">` +
	`<summary>{{T "rastrillo.ui.shell_language"}}<span rst-caret ` +
	`aria-hidden="true">{{icon "chevron-down"}}</span></summary>` +
	`<div rst-dropdown-menu>{{range .Locales}}<a href="{{.Href}}" ` +
	`lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} ` +
	`aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div></details>`

// noScheme closes the locale block and redefines the scheme block to
// render nothing: the shipped default posts to /_theme, and this tree
// is static (TestTheStaticDemosCarryNoSchemeMenu). The gallery's own
// toggle in the bar paints every frame instead. The body is an
// if-false action, not empty, because text/template ignores an empty
// redefinition of a block that has a body.
const noScheme = `{{end}}{{define "scheme"}}{{if false}}{{.}}{{end}}`

// languageRow is the account card's Language row with links in place
// of the locale partial's forms, which post to /_locale, and this tree
// is static: the label, the button that opens the list as a popover,
// the <details> an engine without popovers shows, and the list, in
// that order and touching, as the partial writes them.
const languageRow = `<div rst-menu-row><span rst-menu-row-label>` +
	`{{T "rastrillo.ui.shell_language"}}</span>` +
	`<button type="button" popovertarget="rst-language">` + languageFace +
	`</button><details rst-dropdown name="rst-menus"><summary>` +
	languageFace + `</summary></details>` +
	`<div rst-locale popover id="rst-language"><div rst-menu-row>` +
	`<h2>{{T "rastrillo.ui.shell_language"}}</h2>` +
	`<button type="button" popovertarget="rst-language" ` +
	`popovertargetaction="hide">{{T "rastrillo.ui.close"}}</button></div>` +
	`{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" ` +
	`dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>` +
	`{{.Name}}</a>{{end}}</div></div>`

// languageFace is what the Language row's button and its fallback's
// summary both show: the current language, in its own lang, and a
// caret.
const languageFace = `{{range .Locales}}{{if .Current}}` +
	`<span lang="{{.Code}}">{{.Name}}</span>{{end}}{{end}}` +
	`<span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span>`

// topbarMenuContent is the topbar demo's Menu page, shaped as the
// scaffold's templates/menu.html is: a heading and the nav's links as
// rows. On a phone the shell lays out the bar's nav as the rows and
// this is not shown; on a desktop the shell draws nothing special
// here, so the page lists the links itself.
const topbarMenuContent = `
<header rst-page-header><div rst-page-header-titles>` +
	`<h1>{{T "rastrillo.ui.shell_menu"}}</h1></div></header>
<div rst-list>{{range list (P "Posts") (P "Comments") (P "Settings")}}` +
	`<div rst-row><span rst-row-main><a class="rst-nm" ` +
	`href="{{$.PageHref}}">{{.}}</a></span></div>{{end}}</div>
`
