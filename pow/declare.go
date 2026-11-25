package pow

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name:       "pow_spent_nonces",
		Key:        []string{"nonce"},
		Kind:       migrate.System,
		NotAudited: "replay guard of spent one-time IDs",
	})
}
