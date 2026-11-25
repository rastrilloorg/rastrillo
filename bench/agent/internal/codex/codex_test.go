package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestCodexRepliesCarryTokensAndCommands(t *testing.T) {
	file, err := os.Open("testdata/exec.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	reply := readEvents(file)

	// codex-cli 0.165.0, gpt-6-astra at high effort, asked to run ls:
	// one turn, one command (started and completed: counted once).
	want := Reply{InputTokens: 30882, CachedTokens: 27520, OutputTokens: 116,
		Commands: 1}
	if reply != want {
		t.Errorf("reply = %+v, want %+v", reply, want)
	}
}

// needBwrap skips where bubblewrap cannot make a sandbox at all; Run
// itself refuses to run codex there, so nothing runs unisolated.
func needBwrap(t *testing.T) {
	t.Helper()
	if err := exec.Command("bwrap", "--ro-bind", "/", "/", "true").Run(); err != nil {
		t.Skipf("bwrap cannot run here: %v", err)
	}
}

// fakeCodex puts a codex on PATH that runs script with the -o path in
// $out, and gives Run an auth file to copy.
func fakeCodex(t *testing.T, script string) {
	t.Helper()
	needBwrap(t)
	install := t.TempDir()
	os.Mkdir(filepath.Join(install, "bin"), 0o755)
	os.WriteFile(filepath.Join(install, "bin", "codex"), []byte(`#!/bin/sh
out=
while [ $# -gt 0 ]; do
	case "$1" in -o) out=$2; shift ;; -C) cd "$2" || exit 9; shift ;; esac
	shift
done
cat > /dev/null
`+script+"\n"), 0o755)
	t.Setenv("PATH", filepath.Join(install, "bin")+":"+os.Getenv("PATH"))
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"fake":true}`), 0o600)
	t.Setenv("CODEX_HOME", home)
}

func reader(t *testing.T) Exec {
	return Exec{Model: "gpt-test", Effort: "high", Dir: t.TempDir(),
		Timeout: time.Minute}
}

func TestCodexWithNoAnswerFileIsSilent(t *testing.T) {
	fakeCodex(t, `echo '{"type":"turn.started"}'
echo "the model went quiet" >&2
exit 0`)

	_, err := reader(t).Run(context.Background(), "What status?")

	if !errors.Is(err, ErrSilent) {
		t.Fatalf("err = %v, want ErrSilent", err)
	}
	if !strings.Contains(err.Error(), "the model went quiet") {
		t.Errorf("the error does not carry the log: %v", err)
	}
}

func TestAnEmptyAnswerFileIsSilent(t *testing.T) {
	fakeCodex(t, `printf '  \n' > "$out"`)

	_, err := reader(t).Run(context.Background(), "What status?")

	if !errors.Is(err, ErrSilent) {
		t.Fatalf("err = %v, want ErrSilent", err)
	}
}

func TestCodexAnswersFromItsOutputFile(t *testing.T) {
	fakeCodex(t, `echo '{"type":"item.completed","item":{"id":"i1","type":"command_execution"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":3}}'
printf '{"answer": "303"}' > "$out"`)

	reply, err := reader(t).Run(context.Background(), "What status?")
	if err != nil {
		t.Fatal(err)
	}

	want := Reply{Text: `{"answer": "303"}`, InputTokens: 10, CachedTokens: 4,
		OutputTokens: 3, Commands: 1}
	if reply != want {
		t.Errorf("reply = %+v, want %+v", reply, want)
	}
}

func TestAReaderCannotSeeOutsideItsDirectories(t *testing.T) {
	keys := filepath.Join(t.TempDir(), "keys.json")
	os.WriteFile(keys, []byte(`[{"ID":"x","Answer":"303"}]`), 0o644)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	fakeCodex(t, `cat `+keys+` > /dev/null 2>&1; echo "keys $?" > "$out"
ls `+home+` > /dev/null 2>&1; echo "home $?" >> "$out"
ls "$HOME"/.ssh > /dev/null 2>&1; echo "ssh $?" >> "$out"
cat app/note.txt >> "$out"
echo changed > app/note.txt 2>/dev/null; echo "write $?" >> "$out"`)
	read := reader(t)
	os.Mkdir(filepath.Join(read.Dir, "app"), 0o755)
	os.WriteFile(filepath.Join(read.Dir, "app", "note.txt"), []byte("visible\n"), 0o644)

	reply, err := read.Run(context.Background(), "Look around.")
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("inside the sandbox:\n%s", reply.Text)
	if !strings.Contains(reply.Text, "visible") {
		t.Errorf("the reader cannot see its own directory: %q", reply.Text)
	}
	for _, refused := range []string{"keys 0", "home 0", "ssh 0", "write 0"} {
		if strings.Contains(reply.Text, refused) {
			t.Errorf("the reader could do %q:\n%s", refused, reply.Text)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(read.Dir, "app", "note.txt"))
	if string(raw) != "visible\n" {
		t.Errorf("the reader changed its copy: %q", raw)
	}
}

func TestARefreshedLoginIsKept(t *testing.T) {
	fakeCodex(t, `printf '{"fresh":true}' > "$CODEX_HOME/auth.json"
printf 'ok' > "$out"`)

	if _, err := reader(t).Run(context.Background(), "Hi."); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "auth.json"))
	if string(raw) != `{"fresh":true}` {
		t.Errorf("auth.json = %s; codex's refreshed login was lost", raw)
	}
}

// Only a login codex could have written goes back: never an empty or
// broken file in place of the operator's.
func TestABrokenLoginIsNotKept(t *testing.T) {
	for name, written := range map[string]string{
		"empty": "", "not JSON": `{"fresh":`,
	} {
		t.Run(name, func(t *testing.T) {
			fakeCodex(t, `printf '%s' '`+written+`' > "$CODEX_HOME/auth.json"
printf 'ok' > "$out"`)

			if _, err := reader(t).Run(context.Background(), "Hi."); err != nil {
				t.Fatal(err)
			}

			raw, _ := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"),
				"auth.json"))
			if string(raw) != `{"fake":true}` {
				t.Errorf("auth.json = %q; the operator's login was replaced", raw)
			}
		})
	}
}

// operatorLogin gives the write-back an operator login holding copied,
// and a scratch home where codex refreshed it to refreshed.
func operatorLogin(t *testing.T, copied, refreshed string) (operator,
	home string) {
	t.Helper()
	operatorHome := t.TempDir()
	t.Setenv("CODEX_HOME", operatorHome)
	operator = filepath.Join(operatorHome, "auth.json")
	os.WriteFile(operator, []byte(copied), 0o600)
	home = t.TempDir()
	os.WriteFile(filepath.Join(home, "auth.json"), []byte(refreshed), 0o600)
	return operator, home
}

func holdLoginLock(t *testing.T, operator string) (release func()) {
	t.Helper()
	file, err := os.OpenFile(operator+".bench.lock", os.O_RDWR|os.O_CREATE,
		0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	return func() { file.Close() }
}

// Another bench process holding the login's lock keeps a write-back
// waiting until it lets go.
func TestAWriteBackWaitsForTheLoginLock(t *testing.T) {
	operator, home := operatorLogin(t, `{"old":true}`, `{"fresh":true}`)
	release := holdLoginLock(t, operator)

	done := make(chan struct{})
	go func() {
		keepRefreshedLogin(home, []byte(`{"old":true}`))
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	raw, _ := os.ReadFile(operator)
	if string(raw) != `{"old":true}` {
		t.Errorf("auth.json = %s while another process held the lock", raw)
	}
	release()
	<-done

	raw, _ = os.ReadFile(operator)
	if string(raw) != `{"fresh":true}` {
		t.Errorf("auth.json = %s after the lock was let go", raw)
	}
}

func TestCopyingTheLoginWaitsForTheLoginLock(t *testing.T) {
	operator, _ := operatorLogin(t, `{"old":true}`, `{}`)
	release := holdLoginLock(t, operator)

	done := make(chan struct{})
	go func() {
		scratchHome(filepath.Join(t.TempDir(), "home"))
		close(done)
	}()
	select {
	case <-done:
		t.Error("the login was copied while another process held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	release()
	<-done
}

// A temporary file some earlier write-back left behind does not stop
// this one: each write-back has a temporary file of its own.
func TestALeftoverTemporaryDoesNotLoseTheLogin(t *testing.T) {
	operator, home := operatorLogin(t, `{"old":true}`, `{"fresh":true}`)
	os.Mkdir(operator+".bench", 0o700)

	keepRefreshedLogin(home, []byte(`{"old":true}`))

	raw, _ := os.ReadFile(operator)
	if string(raw) != `{"fresh":true}` {
		t.Errorf("auth.json = %s; the refreshed login was lost", raw)
	}
}

func TestAWriteBackThatFailsIsAnError(t *testing.T) {
	operator, home := operatorLogin(t, `{"old":true}`, `{"fresh":true}`)
	os.WriteFile(operator+".bench.lock", nil, 0o600)
	os.Chmod(filepath.Dir(operator), 0o500)
	t.Cleanup(func() { os.Chmod(filepath.Dir(operator), 0o700) })

	err := keepRefreshedLogin(home, []byte(`{"old":true}`))

	if err == nil {
		t.Error("a write-back that could not write reported no error")
	}
	raw, _ := os.ReadFile(operator)
	if string(raw) != `{"old":true}` {
		t.Errorf("auth.json = %s after a failed write-back", raw)
	}
}

// Readers that all copied one login and all refreshed it leave exactly
// one of their logins in place, whole, and no temporary files.
func TestConcurrentWriteBacksLeaveOneWholeLogin(t *testing.T) {
	operator, _ := operatorLogin(t, `{"old":true}`, `{}`)
	var wait sync.WaitGroup
	refreshed := map[string]bool{}
	for i := range 16 {
		login := fmt.Sprintf(`{"fresh":%d,"pad":%q}`, i,
			strings.Repeat("x", 64<<10))
		refreshed[login] = true
		home := t.TempDir()
		os.WriteFile(filepath.Join(home, "auth.json"), []byte(login), 0o600)
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := keepRefreshedLogin(home, []byte(`{"old":true}`)); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()

	raw, _ := os.ReadFile(operator)
	if !refreshed[string(raw)] {
		t.Errorf("auth.json is not one reader's whole login (%d bytes)", len(raw))
	}
	left, _ := filepath.Glob(operator + ".bench-*")
	if len(left) > 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}
