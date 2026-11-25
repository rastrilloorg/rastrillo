package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
)

// Keys live apart from the kept runs, which are never written to.
func TestKeysAreWrittenApartFromTheRuns(t *testing.T) {
	cache := t.TempDir()
	path := keysPath(cache, "1", "groomer-carlos-1")

	if path != filepath.Join(cache, "scoring", "1", "groomer-carlos-1", "keys.json") {
		t.Errorf("keys go to %s", path)
	}
	if strings.HasPrefix(path, filepath.Join(cache, "1")+"/") {
		t.Errorf("keys go inside the run: %s", path)
	}
}

// now stands for the hash of what the brief and the bench have today.
const now = "now"

// madeKeys saves keys.json for groomer-carlos-1 as made with questions
// and prompt, and gives a keyer whose brief has its questions today.
func madeKeys(t *testing.T, questions, prompt string) (keyer, *keyedRun) {
	t.Helper()
	root, cache := t.TempDir(), t.TempDir()
	briefDir := filepath.Join(root, "briefs", "groomer")
	os.MkdirAll(briefDir, 0o755)
	os.WriteFile(filepath.Join(briefDir, "questions.txt"),
		[]byte("id: dog\nkind: word\nask: Which field?\n"), 0o644)
	if questions == now {
		var err error
		if questions, err = comprehend.QuestionsHash(briefDir); err != nil {
			t.Fatal(err)
		}
	}
	if prompt == now {
		prompt = comprehend.PromptHash()
	}
	path := keysPath(cache, "1", "groomer-carlos-1")
	os.MkdirAll(filepath.Dir(path), 0o755)
	made, _ := json.Marshal(comprehend.KeyFile{
		QuestionsHash: questions, PromptHash: prompt,
		Keys: []comprehend.Key{{ID: "dog", Answer: "dog_name", Settled: true}}})
	os.WriteFile(path, made, 0o644)
	// No claude, no app: anything but reading would fail.
	t.Setenv("PATH", "")
	return keyer{root: root, cache: cache}, &keyedRun{round: "1",
		brief: "groomer", name: "groomer-carlos-1",
		dir: filepath.Join(cache, "1", "groomer-carlos-1")}
}

// A key costs a Claude session: one made before is read, not made again.
func TestKeysMadeBeforeAreKept(t *testing.T) {
	maker, run := madeKeys(t, now, now)

	keys, err := maker.key(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	if len(keys) != 1 || keys[0].Answer != "dog_name" {
		t.Errorf("keys = %+v", keys)
	}
}

// Keys made for other questions, or by another prompt, are not this
// brief's keys today: they are refused, never read as if they were.
func TestKeysMadeOtherwiseAreRefused(t *testing.T) {
	for name, made := range map[string][2]string{
		"other questions":              {"an-older-hash", now},
		"another prompt":               {now, "an-older-prompt"},
		"made before hashes were kept": {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			maker, run := madeKeys(t, made[0], made[1])

			_, err := maker.key(context.Background(), run)

			if err == nil || !strings.Contains(err.Error(), "keys.json") {
				t.Errorf("err = %v, want a refusal naming keys.json", err)
			}
		})
	}
}

func TestKeysFailsWhenAnyRunFailed(t *testing.T) {
	runs := []*keyedRun{
		{round: "1", name: "groomer-carlos-1"},
		{round: "1", name: "groomer-carlos-2", err: errors.New("no JSON")},
	}

	err := failures(runs)

	if err == nil || !strings.Contains(err.Error(), "groomer-carlos-2") ||
		strings.Contains(err.Error(), "groomer-carlos-1") {
		t.Errorf("err = %v, want one naming only groomer-carlos-2", err)
	}
}

// A behavioural key read from before is held to today's check against
// the key-maker's transcript kept beside it: output its request never
// printed, or no transcript at all, unsettles it. No session is made.
func TestBehaviouralKeysMadeBeforeAreCheckedAgain(t *testing.T) {
	evidence := "$ curl -sI http://127.0.0.1:20311/owner\nHTTP/1.1 303 See Other"
	ran := `{"type":"assistant","message":{"id":"m1","content":[` +
		`{"type":"tool_use","id":"t1","name":"Bash","input":` +
		`{"command":"curl -sI http://127.0.0.1:20311/owner"}}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result",` +
		`"tool_use_id":"t1","content":%q}]}}` + "\n"
	for name, test := range map[string]struct {
		transcript string
		settled    bool
	}{
		"its request printed it": {fmt.Sprintf(ran, "HTTP/1.1 303 See Other"),
			true},
		"its request failed": {fmt.Sprintf(ran, "curl: (7) Failed to connect"),
			false},
		"no transcript": {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			maker, run := madeKeys(t, now, now)
			briefDir := filepath.Join(maker.root, "briefs", "groomer")
			os.WriteFile(filepath.Join(briefDir, "questions.txt"),
				[]byte("id: owner\nkind: status\nask: Status?\n"+
					"behavioural: yes\n"), 0o644)
			hash, _ := comprehend.QuestionsHash(briefDir)
			path := keysPath(maker.cache, "1", "groomer-carlos-1")
			made, _ := json.Marshal(comprehend.KeyFile{
				QuestionsHash: hash, PromptHash: comprehend.PromptHash(),
				Keys: []comprehend.Key{{ID: "owner", Answer: "303",
					Evidence: evidence, Settled: true}}})
			os.WriteFile(path, made, 0o644)
			if test.transcript != "" {
				os.WriteFile(filepath.Join(filepath.Dir(path), "keymaker.jsonl"),
					[]byte(test.transcript), 0o644)
			}

			keys, err := maker.key(context.Background(), run)
			if err != nil {
				t.Fatal(err)
			}

			if keys[0].Settled != test.settled {
				t.Errorf("settled = %v, want %v: %+v", keys[0].Settled,
					test.settled, keys[0])
			}
		})
	}
}
