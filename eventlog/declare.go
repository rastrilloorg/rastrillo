package eventlog

import "amadan.net/rastrillo/rastrillo/migrate"

func declare(set *migrate.Set) *migrate.Set {
	return set.Declare(migrate.Data{
		Name: "eventlog",
		Key:  []string{"stream", "writer", "seq"},
		Kind: migrate.Personal,
		NotAudited: "eventlog records actor, kind and time for " +
			"every append itself",
		Erase: migrate.EraseKeep,
		KeepReason: "append-only and copied to other machines; " +
			"erasure is the follow-up spec's",
	}).Declare(migrate.Data{
		Name:       "eventlog_writer",
		Key:        []string{"id"},
		Kind:       migrate.System,
		NotAudited: "one row naming this machine's writer",
	})
}
