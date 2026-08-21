package password

import "testing"

func TestHashVerifyRoundTrip(t *testing.T) {
	enc, err := Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !Verify(enc, "s3cret") {
		t.Errorf("Verify(Hash(%q), %q) = false, want true", "s3cret", "s3cret")
	}
}

func TestVerifyWrongPassword(t *testing.T) {
	enc, err := Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if Verify(enc, "wrong") {
		t.Errorf("Verify with wrong password = true, want false")
	}
}

func TestHashSaltsDiffer(t *testing.T) {
	a, err := Hash("x")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	b, err := Hash("x")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if a == b {
		t.Errorf("two Hash(%q) calls produced identical output %q, want distinct salts", "x", a)
	}
}

func TestVerifyGarbageEncoded(t *testing.T) {
	cases := []string{"", "nonsense", "pbkdf2$sha256$abc$xx$yy"}
	for _, enc := range cases {
		if Verify(enc, "anything") {
			t.Errorf("Verify(%q, ...) = true, want false", enc)
		}
	}
}

func TestParamsPinned(t *testing.T) {
	enc, err := Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	const want = "pbkdf2$sha256$600000$"
	if len(enc) < len(want) || enc[:len(want)] != want {
		t.Errorf("Hash output = %q, want prefix %q", enc, want)
	}
}
