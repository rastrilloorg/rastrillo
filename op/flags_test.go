package op

import (
	"strings"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

func TestCheckFlags(t *testing.T) {
	cases := []struct {
		name       string
		descriptor Descriptor
		wantError  []string
	}{
		{
			name:       "a write that starts with a fence verb",
			descriptor: write("notes.purge", false),
			wantError:  []string{"notes.purge", "Destructive"},
		},
		{
			name:       "the same write once marked",
			descriptor: write("notes.purge_all", true),
		},
		{
			name: "a read is exempt from the fence",
			descriptor: Descriptor{Operation: rastrillo.Operation{
				ID: "orders.sendable_list", Access: rastrillo.Read,
			}},
		},
		{
			name: "a read marked Destructive",
			descriptor: Descriptor{
				Operation: rastrillo.Operation{
					ID: "notes.list", Access: rastrillo.Read,
				},
				Destructive: true,
			},
			wantError: []string{"notes.list", "Destructive"},
		},
		{
			name:       "a write whose verb is resend",
			descriptor: write("tickets.resend", false),
			wantError:  []string{"tickets.resend", "Destructive"},
		},
		{
			name:       "a write that only looks like remove",
			descriptor: write("x.rename", false),
		},
		{
			name:       "only the last part of the ID counts",
			descriptor: write("send.draft_note", false),
		},
	}
	for _, test := range cases {
		err := CheckFlags(test.descriptor)
		if len(test.wantError) == 0 {
			if err != nil {
				t.Errorf("%s: CheckFlags = %v, want nil", test.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: CheckFlags = nil, want an error", test.name)
			continue
		}
		for _, part := range test.wantError {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%s: error %q lacks %q", test.name, err, part)
			}
		}
	}
}

func write(id string, destructive bool) Descriptor {
	return Descriptor{
		Operation:   rastrillo.Operation{ID: id, Access: rastrillo.Write},
		Destructive: destructive,
	}
}
