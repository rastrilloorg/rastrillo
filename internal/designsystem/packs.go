package designsystem

// The gallery is not an app: it has no manifest and no generated
// gen/locales/packs.go to link the formatting data its own twelve
// locales need. moneySampleMarkup calls locales.CheckConfig over
// rastrillo.BaseLocales() directly, and CheckConfig fails closed on an
// offered locale whose pack is not linked (locales/config.go) — so
// without these imports the gallery itself would fail to build its
// own Money sample.
import (
	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/bn"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
	_ "amadan.net/rastrillo/rastrillo/locales/ga"
	_ "amadan.net/rastrillo/rastrillo/locales/hi"
	_ "amadan.net/rastrillo/rastrillo/locales/ja"
	_ "amadan.net/rastrillo/rastrillo/locales/pt"
	_ "amadan.net/rastrillo/rastrillo/locales/ru"
	_ "amadan.net/rastrillo/rastrillo/locales/vi"
	_ "amadan.net/rastrillo/rastrillo/locales/yue"
	_ "amadan.net/rastrillo/rastrillo/locales/zh"
)
