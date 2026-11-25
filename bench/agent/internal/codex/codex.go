// Package codex runs `codex exec` as a reader: one prompt, read-only,
// shut in a bubblewrap sandbox that shows it its directories and
// nothing else of this machine, least of all the answer keys.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Exec is one reader. Inside its sandbox, Dir is /work and AddDirs[i]
// is /add/<i+1>, all read only; prompts should name them relative to
// /work or by those paths.
type Exec struct {
	Model, Effort, Dir string
	AddDirs            []string
	Timeout            time.Duration
}

// Reply is the reader's last message and what it spent getting there.
// InputTokens includes CachedTokens, as codex reports them.
type Reply struct {
	Text                                              string
	InputTokens, CachedTokens, OutputTokens, Commands int
}

// ErrSilent is codex exiting 0 without writing an answer, or writing an
// empty one. It is a harness failure, not an answer.
var ErrSilent = errors.New("codex exited 0 and wrote no answer")

// Run asks once, asking again up to twice if codex goes silent.
func (e Exec) Run(ctx context.Context, prompt string) (Reply, error) {
	var reply Reply
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		reply, err = e.once(ctx, prompt)
		if !errors.Is(err, ErrSilent) {
			break
		}
	}
	return reply, err
}

const (
	insideCodex = "/opt/codex"
	insideHome  = "/codex-home"
	insideWork  = "/work"
	answerFile  = insideHome + "/answer.txt"
)

func (e Exec) once(ctx context.Context, prompt string) (_ Reply, err error) {
	if e.Model == "" || e.Dir == "" {
		return Reply{}, errors.New("codex: an Exec needs a Model and a Dir")
	}
	install, binary, err := installation()
	if err != nil {
		return Reply{}, err
	}
	scratch, err := os.MkdirTemp("", "codex-")
	if err != nil {
		return Reply{}, err
	}
	defer os.RemoveAll(scratch)
	home := filepath.Join(scratch, "home")
	login, err := scratchHome(home)
	if err != nil {
		return Reply{}, err
	}
	defer func() {
		if keepErr := keepRefreshedLogin(home, login); keepErr != nil {
			err = errors.Join(err, keepErr)
		}
	}()

	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}
	var events, stderr bytes.Buffer
	command := exec.CommandContext(ctx, "bwrap",
		e.sandbox(install, home, binary)...)
	command.Stdin = strings.NewReader(prompt)
	command.Stdout, command.Stderr = &events, &stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 10 * time.Second
	exitErr := command.Run()

	log := lastLines(events.String()+stderr.String(), 20)
	reply := readEvents(&events)
	answer, readErr := os.ReadFile(
		filepath.Join(home, filepath.Base(answerFile)))
	switch {
	case exitErr != nil:
		return reply, fmt.Errorf("codex: %w (%v)\n%s", exitErr, ctx.Err(), log)
	case errors.Is(readErr, os.ErrNotExist):
		return reply, fmt.Errorf("%w\n%s", ErrSilent, log)
	case readErr != nil:
		return reply, readErr
	case strings.TrimSpace(string(answer)) == "":
		return reply, fmt.Errorf("%w: the answer file is empty\n%s",
			ErrSilent, log)
	}
	reply.Text = string(answer)
	return reply, nil
}

// sandbox is bwrap's arguments: the system's programs and libraries,
// the few files in /etc that DNS and TLS need, codex's own install, a
// scratch CODEX_HOME, and the reader's directories. The network stays,
// because codex must reach its model; codex's own read-only sandbox
// keeps it from the commands the model runs.
func (e Exec) sandbox(install, home, binary string) []string {
	args := []string{"--unshare-all", "--share-net", "--die-with-parent",
		"--new-session", "--ro-bind", "/usr", "/usr"}
	for _, top := range []string{"/bin", "/sbin", "/lib", "/lib64"} {
		if target, err := os.Readlink(top); err == nil {
			args = append(args, "--symlink", target, top)
		} else {
			args = append(args, "--ro-bind-try", top, top)
		}
	}
	for _, file := range []string{"/etc/resolv.conf", "/etc/hosts",
		"/etc/nsswitch.conf", "/etc/passwd", "/etc/group", "/etc/ssl",
		"/etc/ca-certificates", "/etc/localtime"} {
		args = append(args, "--ro-bind-try", file, file)
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--ro-bind", install, insideCodex,
		"--bind", home, insideHome,
		"--ro-bind", e.Dir, insideWork)
	codexArgs := []string{"exec", "-m", e.Model}
	if e.Effort != "" {
		codexArgs = append(codexArgs, "-c", "model_reasoning_effort="+e.Effort)
	}
	codexArgs = append(codexArgs, "-s", "read-only", "--skip-git-repo-check",
		"-C", insideWork)
	for i, dir := range e.AddDirs {
		inside := fmt.Sprintf("/add/%d", i+1)
		args = append(args, "--ro-bind", dir, inside)
		codexArgs = append(codexArgs, "--add-dir", inside)
	}
	codexArgs = append(codexArgs, "--json", "-o", answerFile, "-")
	args = append(args, "--chdir", insideWork, "--clearenv",
		"--setenv", "PATH",
		insideCodex+"/codex-path:/usr/local/bin:/usr/bin:/bin",
		"--setenv", "HOME", insideHome, "--setenv", "CODEX_HOME", insideHome,
		"--setenv", "TMPDIR", "/tmp", "--setenv", "LANG", "C.UTF-8",
		filepath.Join(insideCodex, binary))
	return append(args, codexArgs...)
}

// installation finds codex on PATH and the directory it was installed
// as: the parent of its bin/, where its bundled bwrap and rg live too.
// binary is codex's path inside that directory.
func installation() (install, binary string, err error) {
	found, err := exec.LookPath("codex")
	if err != nil {
		return "", "", err
	}
	if found, err = filepath.EvalSymlinks(found); err != nil {
		return "", "", err
	}
	install = filepath.Dir(found)
	if filepath.Base(install) == "bin" {
		install = filepath.Dir(install)
	}
	binary, err = filepath.Rel(install, found)
	return install, binary, err
}

// scratchHome makes a CODEX_HOME holding a copy of the operator's login
// and an empty config: none of their instructions, MCP servers or
// skills. It returns the login as copied.
func scratchHome(home string) ([]byte, error) {
	if err := os.Mkdir(home, 0o700); err != nil {
		return nil, err
	}
	login, err := copyLogin()
	if err != nil {
		return nil, fmt.Errorf("codex: no login to copy: %w", err)
	}
	err = os.WriteFile(filepath.Join(home, "auth.json"), login, 0o600)
	if err != nil {
		return nil, err
	}
	return login, os.WriteFile(filepath.Join(home, "config.toml"), nil, 0o600)
}

func copyLogin() ([]byte, error) {
	unlock, err := lockLogin()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return os.ReadFile(loginFile())
}

func loginFile() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "auth.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "auth.json")
}

// lockLogin holds a lock beside the operator's login until unlock. Every
// reader in every bench process takes it to copy the login or put one
// back, so no two of them interleave.
func lockLogin() (unlock func(), err error) {
	file, err := os.OpenFile(loginFile()+".bench.lock",
		os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("codex: locking the login: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("codex: locking the login: %w", err)
	}
	return func() { file.Close() }, nil
}

// keepRefreshedLogin puts back a login codex refreshed in the scratch
// home: a ChatGPT refresh token is used once, so the operator's copy
// would otherwise stop working. A login refreshed meanwhile elsewhere
// is newer, and left alone; so is anything that is not a whole JSON
// document, which codex would not have written. The check and the
// swap happen under the login's lock.
func keepRefreshedLogin(home string, copied []byte) error {
	refreshed, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil || bytes.Equal(refreshed, copied) ||
		len(bytes.TrimSpace(refreshed)) == 0 || !json.Valid(refreshed) {
		return nil
	}
	unlock, err := lockLogin()
	if err != nil {
		return err
	}
	defer unlock()
	operator := loginFile()
	current, err := os.ReadFile(operator)
	if err != nil || !bytes.Equal(current, copied) {
		return nil
	}
	if err := replaceFile(operator, refreshed); err != nil {
		return fmt.Errorf("codex: keeping the refreshed login: %w", err)
	}
	return nil
}

// replaceFile writes content to a temporary file of its own beside path
// and renames it over path, so path is always one whole login.
func replaceFile(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path),
		filepath.Base(path)+".bench-*")
	if err != nil {
		return err
	}
	_, err = temporary.Write(content)
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary.Name(), path)
	}
	if err != nil {
		os.Remove(temporary.Name())
	}
	return err
}

// readEvents adds up `codex exec --json`: tokens from each finished
// turn's usage, and each command the model ran, once.
func readEvents(r io.Reader) Reply {
	var reply Reply
	commands := map[string]bool{}
	reader := bufio.NewReader(r)
	for {
		raw, err := reader.ReadBytes('\n')
		var event struct {
			Type string `json:"type"`
			Item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
			Usage struct {
				Input  int `json:"input_tokens"`
				Cached int `json:"cached_input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(raw, &event) == nil {
			switch {
			case event.Type == "turn.completed":
				reply.InputTokens += event.Usage.Input
				reply.CachedTokens += event.Usage.Cached
				reply.OutputTokens += event.Usage.Output
			case strings.HasPrefix(event.Type, "item.") &&
				event.Item.Type == "command_execution":
				commands[event.Item.ID] = true
			}
		}
		if err != nil {
			break
		}
	}
	reply.Commands = len(commands)
	return reply
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-count):], "\n")
}
