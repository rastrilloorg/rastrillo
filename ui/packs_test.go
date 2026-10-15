package ui

// Boot now refuses an app that links no pack: Handler checks the
// locale configuration, and its default (en when nothing is set) needs
// its pack linked in this test binary as in any app. es and ar are
// linked for the locale functions' tests, which format Spanish money
// and Arabic money with its bidi marks; without their packs both would
// quietly fall back to the default's conventions and prove nothing.
// No build tag, so the browser build links them too.
import (
	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
)
