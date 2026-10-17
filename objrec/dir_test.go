package objrec_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"amadan.net/rastrillo/rastrillo/objrec"
)

func TestDirCreateIsOnce(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	ctx := context.Background()
	if _, err := s.Create(ctx, "users/a.json", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "users/a.json", []byte(`{"n":2}`)); !errors.Is(err, objrec.ErrExists) {
		t.Fatalf("second create: got %v, want ErrExists", err)
	}
	body, _, err := s.Get(ctx, "users/a.json")
	if err != nil || string(body) != `{"n":1}` {
		t.Fatalf("get: %q %v — the loser must not have overwritten the winner", body, err)
	}
}

func TestDirCreateRaceHasOneWinner(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Create(context.Background(), "emails/x.json", []byte(`{}`))
			switch {
			case err == nil:
				wins.Add(1)
			case !errors.Is(err, objrec.ErrExists):
				t.Errorf("loser got %v, want ErrExists", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners, want exactly 1", wins.Load())
	}
}

func TestDirUpdateRefusesAStaleVersion(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	ctx := context.Background()
	v1, _ := s.Create(ctx, "k.json", []byte(`1`))
	if _, err := s.Update(ctx, "k.json", []byte(`2`), v1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, "k.json", []byte(`3`), v1); !errors.Is(err, objrec.ErrChanged) {
		t.Fatalf("stale update: got %v, want ErrChanged", err)
	}
}

func TestDirUpdateOfAMissingRecordIsNotFound(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	ctx := context.Background()
	v, _ := s.Create(ctx, "k.json", []byte(`1`))
	if err := s.Delete(ctx, "k.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, "k.json", []byte(`2`), v); !errors.Is(err, objrec.ErrNotFound) {
		t.Fatalf("update after delete: got %v, want ErrNotFound (ErrChanged would make Mutate spin)", err)
	}
}

func TestDirDeleteIsIdempotent(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	if err := s.Delete(context.Background(), "never.json"); err != nil {
		t.Fatalf("deleting what is not there: %v", err)
	}
}

func TestDirListPagesInOrder(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	ctx := context.Background()
	for _, k := range []string{"m/c", "m/a", "m/b", "n/a"} {
		if _, err := s.Create(ctx, k, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	cursor := ""
	for {
		keys, next, err := s.List(ctx, "m/", cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, keys...)
		if next == "" {
			break
		}
		cursor = next
	}
	if strings.Join(got, ",") != "m/a,m/b,m/c" {
		t.Fatalf("list: %v", got)
	}
}

func TestKeysAreRefusedBeforeAnyWrite(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	for _, k := range []string{"", "/x", "../x", "a/../b", "A", "a b", "a//b", "a/", strings.Repeat("a", 513)} {
		if _, err := s.Create(context.Background(), k, []byte(`{}`)); !errors.Is(err, objrec.ErrKey) {
			t.Errorf("Create(%q): got %v, want ErrKey", k, err)
		}
	}
}

func TestOversizedRecordIsRefused(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	big := make([]byte, objrec.MaxRecord+1)
	if _, err := s.Create(context.Background(), "big", big); !errors.Is(err, objrec.ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}
