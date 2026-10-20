. "$(dirname "$0")/lib.sh"
# Every shell already offers Light, Dark and System. The right answer
# adds no toggle of its own. Tests are exempt: an agent that confirms
# the shipped switch works by asserting on data-theme got it right.
refuse "no hand-rolled scheme toggle" 'prefers-color-scheme|localStorage|matchMedia|data-theme' internal ':!**/*_test.go'
if git grep -q 'scheme-menu' -- internal; then echo "ok: the shell's scheme-menu is still there"; else echo "FAIL: scheme-menu removed"; status=1; fi
exit $status
