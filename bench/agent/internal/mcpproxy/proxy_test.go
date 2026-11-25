package mcpproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is an MCP server that answers every request with its own
// generation, so a test can see which child answered. A tools/call
// whose name is "hold" is answered only when release is closed.
type fake struct {
	mutex    sync.Mutex
	started  int
	received map[int][]string // generation -> methods, in order
	stopped  map[int]bool
	release  chan struct{}
	// failStart makes that generation fail to start; silent makes it
	// never answer anything.
	failStart, silent int
	outs              map[int]*io.PipeWriter
}

func newFake() *fake {
	return &fake{received: map[int][]string{}, stopped: map[int]bool{},
		release: make(chan struct{}), outs: map[int]*io.PipeWriter{}}
}

func (f *fake) start(dir string) (Child, error) {
	f.mutex.Lock()
	f.started++
	generation := f.started
	if generation == f.failStart {
		f.mutex.Unlock()
		return Child{}, errors.New("no gopls today")
	}
	silent := generation == f.silent
	toChild, fromClient := io.Pipe()
	toClient, fromChild := io.Pipe()
	f.outs[generation] = fromChild
	f.mutex.Unlock()
	var writing sync.Mutex
	write := func(message map[string]any) {
		line, _ := json.Marshal(message)
		writing.Lock()
		fromChild.Write(append(line, '\n'))
		writing.Unlock()
	}
	go func() {
		defer fromChild.Close()
		scanner := bufio.NewScanner(toChild)
		for scanner.Scan() {
			var message struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			json.Unmarshal(scanner.Bytes(), &message)
			f.mutex.Lock()
			f.received[generation] = append(f.received[generation], message.Method)
			f.mutex.Unlock()
			result := map[string]any{"generation": generation, "dir": dir}
			switch {
			case message.ID == nil, silent:
			case message.Method == "subscriptions/listen":
				write(map[string]any{"jsonrpc": "2.0",
					"method": "notifications/subscriptions/acknowledged"})
			case message.Params.Name == "hold":
				go func() {
					<-f.release
					write(map[string]any{"jsonrpc": "2.0", "id": message.ID,
						"result": result})
				}()
			default:
				write(map[string]any{"jsonrpc": "2.0", "id": message.ID,
					"result": result})
			}
		}
	}()
	return Child{In: fromClient, Out: toClient, Stop: func() {
		fromClient.Close()
		f.mutex.Lock()
		f.stopped[generation] = true
		f.mutex.Unlock()
	}}, nil
}

func (f *fake) methods(generation int) []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return append([]string(nil), f.received[generation]...)
}

func (f *fake) starts() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.started
}

// client is Claude Code's side of the proxy.
type client struct {
	t       *testing.T
	in      *io.PipeWriter
	replies chan map[string]any
	done    chan error
}

func connect(t *testing.T, f *fake, dir string) *client {
	t.Helper()
	return connectLogging(t, f, dir, io.Discard)
}

func connectLogging(t *testing.T, f *fake, dir string, log io.Writer) *client {
	t.Helper()
	return connectWith(t, f, dir, log, func(*Proxy) {})
}

func connectWith(
	t *testing.T, f *fake, dir string, log io.Writer, configure func(*Proxy),
) *client {
	t.Helper()
	clientIn, toProxy := io.Pipe()
	fromProxy, clientOut := io.Pipe()
	c := &client{t: t, in: toProxy, replies: make(chan map[string]any, 100),
		done: make(chan error, 1)}
	proxy := &Proxy{Start: f.start, Dir: dir, Poll: 10 * time.Millisecond,
		Settle: 60 * time.Millisecond, ReplayTimeout: 200 * time.Millisecond,
		Log: log}
	configure(proxy)
	go func() {
		c.done <- proxy.Run(context.Background(), clientIn, clientOut)
		clientOut.Close()
	}()
	go func() {
		scanner := bufio.NewScanner(fromProxy)
		for scanner.Scan() {
			var reply map[string]any
			json.Unmarshal(scanner.Bytes(), &reply)
			c.replies <- reply
		}
		close(c.replies)
	}()
	t.Cleanup(func() { toProxy.Close() })
	return c
}

func (c *client) send(line string) {
	c.t.Helper()
	if _, err := c.in.Write([]byte(line + "\n")); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) next() map[string]any {
	c.t.Helper()
	select {
	case reply, open := <-c.replies:
		if !open {
			c.t.Fatal("the proxy closed its output")
		}
		return reply
	case <-time.After(5 * time.Second):
		c.t.Fatal("no reply within 5s")
	}
	return nil
}

// generation is which child answered a reply.
func generation(reply map[string]any) int {
	result, _ := reply["result"].(map[string]any)
	number, _ := result["generation"].(float64)
	return int(number)
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	initialize  = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`
	initialized = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
)

func call(id int, name string) string {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id,
		"method": "tools/call", "params": map[string]any{"name": name}})
	return string(line)
}

// The client starts one session and keeps it: after go.mod appears, its
// requests reach a fresh gopls started in the module, which was given
// the client's initialize and initialized again, and the client never
// sees a second initialize reply.
func TestTheClientSeesOneSessionAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	c := connect(t, f, dir)

	c.send(initialize)
	if reply := c.next(); reply["id"] != 1.0 || generation(reply) != 1 {
		t.Fatalf("initialize reply = %v", reply)
	}
	c.send(initialized)
	c.send(call(2, "go_workspace"))
	if reply := c.next(); reply["id"] != 2.0 || generation(reply) != 1 {
		t.Fatalf("first call = %v, want gopls 1", reply)
	}

	writeGoMod(t, dir)
	waitFor(t, "a second gopls", func() bool { return len(f.methods(2)) >= 2 })
	c.send(call(3, "go_workspace"))
	reply := c.next()
	if reply["id"] != 3.0 || generation(reply) != 2 {
		t.Errorf("call after go.mod = %v, want gopls 2", reply)
	}
	if got := strings.Join(f.methods(2), ","); got !=
		"initialize,notifications/initialized,tools/call" {
		t.Errorf("gopls 2 received %s, want the session replayed first", got)
	}
	waitFor(t, "gopls 1 to stop", func() bool {
		f.mutex.Lock()
		defer f.mutex.Unlock()
		return f.stopped[1]
	})

	c.in.Close()
	for extra := range c.replies {
		t.Errorf("the client saw a reply it never asked for: %v", extra)
	}
	if err := <-c.done; err != nil {
		t.Errorf("Run = %v", err)
	}
}

// A request gopls is still working on when go.mod appears is answered by
// the gopls that took it; the restart waits for it.
func TestARequestInFlightIsNotLost(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	c := connect(t, f, dir)
	c.send(initialize)
	c.next()
	c.send(initialized)
	c.send(call(2, "hold"))
	waitFor(t, "the held call", func() bool { return len(f.methods(1)) == 3 })

	writeGoMod(t, dir)
	time.Sleep(100 * time.Millisecond) // ten polls
	if f.starts() != 1 {
		t.Fatal("gopls restarted with a request in flight")
	}
	close(f.release)
	if reply := c.next(); reply["id"] != 2.0 || generation(reply) != 1 {
		t.Fatalf("held call = %v, want gopls 1's answer", reply)
	}
	waitFor(t, "a second gopls", func() bool { return len(f.methods(2)) >= 2 })
	c.send(call(3, "go_workspace"))
	if reply := c.next(); reply["id"] != 3.0 || generation(reply) != 2 {
		t.Errorf("call after the restart = %v, want gopls 2", reply)
	}
}

// Claude Code 2.1.321 speaks MCP 2026-07-28: no initialize, and a
// subscriptions/listen request that stays open for the whole session.
// It is not in flight, so it cannot hold a restart off; it is given to
// the new gopls, whose acknowledgement the client has already had.
func TestAnOpenSubscriptionIsCarriedAcross(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	c := connect(t, f, dir)
	c.send(`{"jsonrpc":"2.0","id":"listen:0","method":"subscriptions/listen","params":{}}`)
	if reply := c.next(); reply["method"] != "notifications/subscriptions/acknowledged" {
		t.Fatalf("listen = %v, want an acknowledgement", reply)
	}
	c.send(call(1, "go_workspace"))
	if reply := c.next(); generation(reply) != 1 {
		t.Fatalf("first call = %v", reply)
	}

	writeGoMod(t, dir)
	waitFor(t, "a second gopls", func() bool { return len(f.methods(2)) >= 1 })
	c.send(call(2, "go_workspace"))
	reply := c.next()
	if reply["id"] != 2.0 || generation(reply) != 2 {
		t.Errorf("call after go.mod = %v, want gopls 2", reply)
	}
	if got := strings.Join(f.methods(2), ","); got != "subscriptions/listen,tools/call" {
		t.Errorf("gopls 2 received %s", got)
	}
}

// A module that is there from the start (the follow-up resumes in a
// built app) needs no restart; gopls starts in it.
func TestAnAppWithAModuleNeedsNoRestart(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "hello")
	os.MkdirAll(app, 0o755)
	writeGoMod(t, app)
	f := newFake()
	c := connect(t, f, dir)
	c.send(call(1, "go_workspace"))
	reply := c.next()
	result, _ := reply["result"].(map[string]any)
	if result["dir"] != app {
		t.Errorf("gopls started in %v, want the module %s", result["dir"], app)
	}
	time.Sleep(50 * time.Millisecond)
	if f.starts() != 1 {
		t.Errorf("gopls started %d times, want once", f.starts())
	}
}

// syncBuffer is a log the proxy and the test both touch.
type syncBuffer struct {
	mutex sync.Mutex
	text  strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.text.Write(p)
}

func (b *syncBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.text.String()
}

func troubleIn(log *syncBuffer) bool {
	return strings.Contains(log.String(), TroubleMarker)
}

// A gopls that cannot start again leaves the session on the one that
// was running, and says so in the log, where the bench looks.
func TestARestartThatFailsKeepsTheOldGoplsAndSaysSo(t *testing.T) {
	for name, configure := range map[string]func(*fake){
		"start fails":       func(f *fake) { f.failStart = 2 },
		"replay unanswered": func(f *fake) { f.silent = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			f := newFake()
			configure(f)
			log := &syncBuffer{}
			c := connectLogging(t, f, dir, log)
			c.send(initialize)
			c.next()
			c.send(initialized)

			writeGoMod(t, dir)
			waitFor(t, "the trouble line", func() bool { return troubleIn(log) })
			c.send(call(2, "go_workspace"))
			if reply := c.next(); reply["id"] != 2.0 || generation(reply) != 1 {
				t.Errorf("call after a failed restart = %v, want gopls 1", reply)
			}
		})
	}
}

// A gopls that dies mid-session ends the session, and says so.
func TestAGoplsThatDiesIsInTheLog(t *testing.T) {
	f := newFake()
	log := &syncBuffer{}
	c := connectLogging(t, f, t.TempDir(), log)
	c.send(call(1, "go_workspace"))
	c.next()
	f.mutex.Lock()
	f.outs[1].Close()
	f.mutex.Unlock()

	select {
	case err := <-c.done:
		if err == nil {
			t.Error("Run ended without an error when gopls died")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run went on after gopls died")
	}
	if !troubleIn(log) {
		t.Errorf("gopls's death is not in the log:\n%s", log)
	}
}

// A request the client cancels is no longer in flight, so it cannot
// hold a restart off for ever.
func TestACancelledRequestIsNotInFlight(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	c := connect(t, f, dir)
	c.send(call(1, "hold"))
	waitFor(t, "the held call", func() bool { return len(f.methods(1)) == 1 })
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)

	writeGoMod(t, dir)
	waitFor(t, "a second gopls", func() bool { return f.starts() == 2 })
	close(f.release)
}

// gopls loads the module when it starts, so the restart waits until
// go.mod and go.sum stop changing: a scaffold writes them more than once.
// The test's ticks are the proxy's clock, so a loaded machine that
// stalls the writer cannot pass for a module that settled.
func TestTheRestartWaitsForTheModuleToSettle(t *testing.T) {
	const settle, step = 60 * time.Millisecond, 15 * time.Millisecond
	dir := t.TempDir()
	f := newFake()
	var mutex sync.Mutex
	clock := time.Now()
	now := func() time.Time {
		mutex.Lock()
		defer mutex.Unlock()
		return clock
	}
	ticks := make(chan time.Time)
	connectWith(t, f, dir, io.Discard, func(p *Proxy) {
		p.Settle, p.ticks, p.now = settle, ticks, now
	})
	waitFor(t, "the first gopls", func() bool { return f.starts() == 1 })
	// poll moves the clock on a step, then ticks twice: the second tick
	// is taken only once the first, and any restart, is done. The clock
	// moves before each write, and the proxy reads it after looking at
	// the files, so a write is never seen at an earlier time than its own.
	poll := func(write func()) {
		mutex.Lock()
		clock = clock.Add(step)
		mutex.Unlock()
		write()
		ticks <- time.Time{}
		ticks <- time.Time{}
	}
	for i := range 10 {
		poll(func() {
			os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte(fmt.Sprintf("module hello\n// %d\n", i)), 0o644)
			os.WriteFile(filepath.Join(dir, "go.sum"),
				[]byte(strings.Repeat("x", i)), 0o644)
		})
		if f.starts() != 1 {
			t.Fatalf("gopls restarted after write %d, while go.mod was "+
				"still changing", i)
		}
	}
	lastWrite := now()
	for f.starts() == 1 && now().Sub(lastWrite) < 2*settle {
		poll(func() {})
	}
	if quiet := now().Sub(lastWrite); f.starts() != 2 || quiet != settle {
		t.Fatalf("gopls started %d times, %s after the last write; "+
			"want a restart once go.mod was unchanged for %s",
			f.starts(), quiet, settle)
	}
}

// A request gopls never answers cannot hold the restart off for the
// whole session: after RestartWait, the restart gives up, and says so.
func TestARestartWaitsForRequestsInFlightOnlySoLong(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	log := &syncBuffer{}
	c := connectWith(t, f, dir, log, func(p *Proxy) {
		p.RestartWait = 100 * time.Millisecond
	})
	c.send(call(1, "hold"))
	waitFor(t, "the held call", func() bool { return len(f.methods(1)) == 1 })

	writeGoMod(t, dir)
	waitFor(t, "the trouble line", func() bool { return troubleIn(log) })

	if f.starts() != 1 {
		t.Errorf("gopls started %d times with a request in flight", f.starts())
	}
	if !strings.Contains(log.String(), "still in flight") {
		t.Errorf("the log does not say why:\n%s", log)
	}
	close(f.release)
}

// A session that ends while the restart waits has had no gopls in its
// module, and says so.
func TestASessionThatEndsBeforeTheRestartSaysSo(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	log := &syncBuffer{}
	c := connectLogging(t, f, dir, log)
	c.send(call(1, "hold"))
	waitFor(t, "the held call", func() bool { return len(f.methods(1)) == 1 })
	writeGoMod(t, dir)
	waitFor(t, "the restart to wait", func() bool {
		return strings.Contains(log.String(), "restarting gopls once")
	})

	c.in.Close()

	waitFor(t, "the trouble line", func() bool { return troubleIn(log) })
	close(f.release)
}

// Each gopls that runs in the module says so, with the module: one
// started in it, and one restarted in it.
func TestTheLogSaysWhenGoplsHasTheModule(t *testing.T) {
	dir := t.TempDir()
	f := newFake()
	log := &syncBuffer{}
	connectLogging(t, f, dir, log)
	waitFor(t, "the first gopls", func() bool { return f.starts() == 1 })
	if strings.Contains(log.String(), "module active") {
		t.Errorf("a gopls with no module is logged as having one:\n%s", log)
	}
	writeGoMod(t, dir)
	waitFor(t, "the module active line", func() bool {
		return strings.Contains(log.String(),
			"goplsmcp: module active: gopls 2 in "+dir+"\n")
	})

	built := t.TempDir()
	writeGoMod(t, built)
	resumed := &syncBuffer{}
	connectLogging(t, newFake(), built, resumed)
	waitFor(t, "the module active line", func() bool {
		return strings.Contains(resumed.String(),
			"goplsmcp: module active: gopls 1 in "+built+"\n")
	})
}

// An agent that scaffolds into a subdirectory and then moves the module
// up leaves gopls in a directory that is gone: gopls follows the module.
func TestGoplsFollowsAModuleThatMoves(t *testing.T) {
	dir := t.TempDir()
	scaffolded := filepath.Join(dir, "hello")
	os.MkdirAll(scaffolded, 0o755)
	f := newFake()
	c := connect(t, f, dir)
	waitFor(t, "the first gopls", func() bool { return f.starts() == 1 })
	writeGoMod(t, scaffolded)
	waitFor(t, "gopls in the scaffold", func() bool { return f.starts() == 2 })

	os.Rename(filepath.Join(scaffolded, "go.mod"), filepath.Join(dir, "go.mod"))
	os.Remove(scaffolded)
	waitFor(t, "gopls in the moved module", func() bool { return f.starts() == 3 })

	c.send(call(1, "go_workspace"))
	reply := c.next()
	result, _ := reply["result"].(map[string]any)
	if generation(reply) != 3 || result["dir"] != dir {
		t.Errorf("call after the move = %v, want gopls 3 in %s", reply, dir)
	}
}
