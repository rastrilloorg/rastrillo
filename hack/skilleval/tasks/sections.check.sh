. "$(dirname "$0")/lib.sh"
# Three sections a person moves between want a shell with navigation:
# sidebar, topbar or console. On a phone each shows the sections as an
# index page one level up from every other page, under one header row
# (the way up, the app's name, the account button). The miss this task
# exists for is a Menu button in the header: a drawer, a hamburger or
# the old Menu card holding the nav.
#
# Only templates (.html, .htm) are read here: tokens.css names every
# shell attribute as a selector, and the compiled templates' Go repeats
# the markup.
only='*.htm*'
present "a shell with navigation" '<[^>]*[[:space:]]rst-shell-(sidebar|console|topbar)([[:space:]=>/]|$)' internal
present "an index page marked with view" 'define "view" *-?\}\}[[:space:]]*index' internal
# The new account card is <details name="rst-shell-menu">: a value, not
# the old Menu card's attribute, which is why a space must precede it.
refuse "no Menu card, back strip or hamburger" \
	'[[:space:]]rst-shell-(menu|back|tail)([[:space:]=>/]|$)' \
	'icon[[:space:]]+"(menu|hamburger|align-justify)"' '☰' -- internal
# The scheme control belongs in the account card (rst-shell-account in a
# bar, rst-shell-profile in the sidebar's rail), as the shipped layouts
# put it. Whether one element sits inside another is the HTML's to say,
# not a line pattern's, so the harness's own binary reads the templates
# with a tokenizer (hack/skilleval/incard.go).
"${SKILLEVAL_BIN:?the harness sets SKILLEVAL_BIN to its own binary}" incard internal
case $? in
0) echo "ok: the scheme control is in the account card" ;;
1) echo "FAIL: the scheme control is in the account card"; status=1 ;;
*) echo "the templates could not be read"; exit 2 ;;
esac
# A hand-rolled scheme toggle, refused in any file as scheme's check
# refuses it, less data-theme: a second layout beside the first (a
# stage page for sign-in, say) writes it once more, rightly.
only=
notests=1
refuse "no hand-rolled scheme toggle" 'prefers-color-scheme' 'localStorage' 'matchMedia' -- internal
exit $status
