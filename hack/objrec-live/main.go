// Command objrec-live proves, from inside a platform instance, that the
// app's store honours conditional writes: GET /run races 16 creates at
// one key (exactly one must win), then checks a stale If-Match is
// refused and an If-Match on a deleted key is not-found. It writes under
// records/live-<random>/ and deletes what it wrote. Throwaway: deploy,
// curl /run once, delete the app.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/objrec"

	// Serve refuses to start without the default locale's pack linked.
	_ "amadan.net/rastrillo/rastrillo/locales/en"
)

type result struct {
	OK     bool     `json:"ok"`
	Checks []string `json:"checks"`
	Error  string   `json:"error,omitempty"`
}

func run(ctx context.Context, s objrec.Store) result {
	var r result
	fail := func(msg string) result { r.Error = msg; return r }
	b := make([]byte, 6)
	rand.Read(b)
	dir := "live-" + hex.EncodeToString(b) + "/"
	defer func() {
		keys, _, _ := s.List(ctx, dir, "", 1000)
		for _, k := range keys {
			s.Delete(ctx, k)
		}
	}()

	var wins atomic.Int32
	var other atomic.Value
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Create(ctx, dir+"race.json", []byte(`{}`)); err == nil {
				wins.Add(1)
			} else if !errors.Is(err, objrec.ErrExists) {
				other.Store(err.Error())
			}
		}()
	}
	wg.Wait()
	if e, _ := other.Load().(string); e != "" {
		return fail("racing create: " + e)
	}
	if wins.Load() != 1 {
		return fail("racing creates: " + strconv.Itoa(int(wins.Load())) + " winners, want 1")
	}
	r.Checks = append(r.Checks, "16 racing creates, exactly one winner")

	v1, err := s.Create(ctx, dir+"cas.json", []byte(`1`))
	if err != nil {
		return fail("create: " + err.Error())
	}
	if _, err := s.Update(ctx, dir+"cas.json", []byte(`2`), v1); err != nil {
		return fail("fresh update: " + err.Error())
	}
	if _, err := s.Update(ctx, dir+"cas.json", []byte(`3`), v1); !errors.Is(err, objrec.ErrChanged) {
		return fail("stale If-Match was not refused: " + errString(err))
	}
	r.Checks = append(r.Checks, "stale If-Match refused")

	if err := s.Delete(ctx, dir+"cas.json"); err != nil {
		return fail("delete: " + err.Error())
	}
	if _, err := s.Update(ctx, dir+"cas.json", []byte(`4`), v1); !errors.Is(err, objrec.ErrNotFound) {
		return fail("If-Match on a deleted key: " + errString(err))
	}
	r.Checks = append(r.Checks, "If-Match on a deleted key is not found")
	r.OK = true
	return r
}

func errString(err error) string {
	if err == nil {
		return "no error"
	}
	return err.Error()
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	store, err := objrec.S3FromEnv()
	if err != nil {
		logger.Error("store", "err", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /run", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if store == nil {
			json.NewEncoder(w).Encode(result{Error: "no CARLOS_STORE_BUCKET: the store is not delivered to this app"})
			return
		}
		json.NewEncoder(w).Encode(run(r.Context(), store))
	})
	opts, err := rastrillo.Resolve(rastrillo.Options{Mux: mux, Logger: logger})
	if err != nil {
		logger.Error("resolve", "err", err)
		os.Exit(1)
	}
	if err := rastrillo.Serve(opts); err != nil {
		logger.Error("serve", "err", err)
		os.Exit(1)
	}
}
