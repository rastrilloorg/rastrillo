package auth

// Boot now refuses an app that links no pack: Handler checks the
// locale configuration, and its default (en when nothing is set) needs
// its pack linked in this test binary as in any app.
import _ "amadan.net/rastrillo/rastrillo/locales/en"
