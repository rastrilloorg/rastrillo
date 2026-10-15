package currency

import "testing"

// Decision 6 is invisible in today's data: every modern alphaNextToNumber
// pattern equals the standard one plus the spacing rule, so dropping the
// alpha branch changes no row of the ICU grid. This pins the rule on
// synthetic patterns instead, including the side flip no modern locale
// has yet, which follows CLDR rather than ICU.
func TestPatternChoice(t *testing.T) {
	after := moneyFmt{standard: "#,##0.00 ¤", alpha: "¤ #,##0.00"}
	before := moneyFmt{standard: "¤#,##0.00", alpha: "¤ #,##0.00"}
	for _, c := range []struct {
		mf   moneyFmt
		sym  string
		want string
	}{
		{after, "€", after.standard},
		{after, "CHF", after.alpha}, // the letter touches the number: CLDR's alpha pattern, symbol moved
		{after, "​", after.standard},
		{before, "US$", before.standard},
		{before, "CHF", before.alpha},
		{before, "", before.standard},
	} {
		if got := c.mf.pattern(c.sym); got != c.want {
			t.Errorf("%+v.pattern(%q) = %q, want %q", c.mf, c.sym, got, c.want)
		}
	}
}
