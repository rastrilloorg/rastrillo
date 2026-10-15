module amadan.net/rastrillo/rastrillo/hack/moneycldr

go 1.25.0

require (
	amadan.net/rastrillo/rastrillo v0.0.0
	amadan.net/rastrillo/rastrillo/money v0.1.0
)

replace (
	amadan.net/rastrillo/rastrillo => ../..
	amadan.net/rastrillo/rastrillo/money => ../../money
)

require golang.org/x/text v0.39.0
