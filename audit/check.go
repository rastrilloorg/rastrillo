package audit

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/migrate"
)

// CheckSchema fails t once for each way s declares its tables badly, so
// an app's own tests catch an undeclared table before it ships.
func CheckSchema(t testing.TB, set *migrate.Set) {
	t.Helper()
	problems, err := migrate.CheckDeclarations(context.Background(), set)
	if err != nil {
		t.Fatalf("audit: checking declarations: %v", err)
		return
	}
	for _, problem := range problems {
		t.Errorf("%s: %s", problem.Table, problem.Message)
	}
}

// Exemption is one table that declared itself outside a rule.
type Exemption struct {
	Table, Namespace string
	// What is "not audited" or "kept on erasure".
	What   string
	Reason string
}

// Exemptions lists every reason a table gave for being left out of the
// audit log or kept on erasure, so a reviewer can read them in one place.
func Exemptions(set *migrate.Set) []Exemption {
	var list []Exemption
	for _, decl := range set.Declarations() {
		if decl.NotAudited != "" {
			list = append(list, Exemption{
				decl.Name, decl.Namespace, "not audited", decl.NotAudited,
			})
		}
		if decl.Erase == migrate.EraseKeep {
			list = append(list, Exemption{
				decl.Name, decl.Namespace, "kept on erasure", decl.KeepReason,
			})
		}
	}
	slices.SortStableFunc(list, func(first, second Exemption) int {
		return cmp.Or(
			strings.Compare(first.Namespace, second.Namespace),
			strings.Compare(first.Table, second.Table),
			strings.Compare(first.What, second.What),
		)
	})
	return list
}
