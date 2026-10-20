. "$(dirname "$0")/lib.sh"
# Every shell already offers Light, Dark and System. The right answer
# adds no toggle of its own. Tests are exempt: an agent that confirms
# the shipped switch works by asserting on data-theme got it right.
notests=1
refuse "no hand-rolled scheme toggle" 'prefers-color-scheme' 'localStorage' 'matchMedia' 'data-theme' -- internal
# Presence is read from production code too: a test that sets Scheme
# itself proves nothing about the renderer.
present "the shell's scheme-menu is still there" 'scheme-menu' internal
# Without Scheme in the page data the layout cannot write data-theme,
# and a chosen Dark paints System on every reload.
present "render still passes Scheme to the layout" 'page\["Scheme"\]|"Scheme"\]? *=|"?Scheme"? *:' internal
exit $status
