package objrec_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo/objrec"
)

type counter struct{ N int }

func TestJSONRoundTrip(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	ctx := context.Background()
	v, err := objrec.CreateJSON(ctx, s, "c.json", counter{N: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, v2, err := objrec.GetJSON[counter](ctx, s, "c.json")
	if err != nil || got.N != 1 || v2 != v {
		t.Fatalf("get: %+v %q %v", got, v2, err)
	}
}

func TestMutateLosesNoIncrement(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := objrec.Mutate(ctx, s, "c.json", func(cur counter, _ bool) (counter, error) {
				cur.N++
				return cur, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, _, _ := objrec.GetJSON[counter](ctx, s, "c.json")
	if got.N != 20 {
		t.Fatalf("N = %d, want 20: a lost update got through", got.N)
	}
}

func TestMutateStopsOnTheCallersError(t *testing.T) {
	s := objrec.Dir(t.TempDir())
	boom := errors.New("boom")
	_, _, err := objrec.Mutate(context.Background(), s, "c.json", func(counter, bool) (counter, error) {
		return counter{}, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the callback's error", err)
	}
}
