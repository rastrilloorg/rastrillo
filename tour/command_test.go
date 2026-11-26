package tour

import "testing"

func TestANestedCommandIsCheckedAgainstItsOwnHelp(t *testing.T) {
	toolchain := thisCheckout(t)
	for command, want := range map[string]bool{
		"rastrillo migration generate --allow-destructive": true,
		"rastrillo migration generate --no-such-flag":      false,
		"rastrillo new --scaffold accounts chat":           true,
		"rastrillo budget code --lower":                    true,
		"rastrillo budget code --allow-destructive":        false,
	} {
		problem := checkCommand(command, toolchain)
		if (problem == "") != want {
			t.Errorf("%s: problem %q, want ok %v", command, problem, want)
		}
	}
}
