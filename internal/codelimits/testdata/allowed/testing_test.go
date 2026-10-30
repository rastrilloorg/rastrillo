package allowed

import "testing"

// TestLong is a test with a 60-line body: tests are exempt from the
// function limit.
func TestLong(t *testing.T) {
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
	t.Log("line")
}

func helper(t testing.TB) { t.Helper() }

func BenchmarkSmall(b *testing.B) { b.ReportAllocs() }

func FuzzSmall(f *testing.F) { f.Add("seed") }
