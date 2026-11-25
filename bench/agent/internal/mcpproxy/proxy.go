// Package mcpproxy stands between Claude Code and a `gopls mcp` child,
// so that gopls can work on an app whose go.mod does not exist yet.
//
// gopls loads the module it starts in, and watches for new ones only in
// directories the client names as roots, which it asks for only after
// the classic MCP initialize. Claude Code 2.1.321 speaks MCP 2026-07-28,
// which has no initialize, so a gopls started in the empty app never
// sees the module the agent scaffolds. The proxy starts gopls again in
// that module once its go.mod has settled, behind the same session: the
// client's own session messages are given to the new child, the
// child's replies to them are dropped, and forwarding resumes.
//
// Anything that leaves the session without the gopls it should have (a
// child that dies, a restart that fails or never comes) is logged with
// TroubleMarker, so the bench can refuse the run. Each time a gopls
// starts in the module, the log says so with ModuleActiveMarker and the
// module's directory, so the bench can require it.
package mcpproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TroubleMarker starts every log line that says gopls was not there
// for part of the session.
const TroubleMarker = "goplsmcp: trouble: "

// ModuleActiveMarker starts the log line that says a gopls is running
// in a module: the line ends with " in " and the module's directory.
const ModuleActiveMarker = "goplsmcp: module active: "

// DefaultSettle is Settle when it is zero.
const DefaultSettle = 3 * time.Second

// Child is one running MCP server: its stdin, its stdout, and how to
// stop it.
type Child struct {
	In   io.WriteCloser
	Out  io.Reader
	Stop func()
}

type Proxy struct {
	// Start runs a server in dir.
	Start func(dir string) (Child, error)
	// Dir is the app's directory; go.mod is looked for in it and up to
	// three levels below.
	Dir string
	// Poll is how often Dir is looked at for a module, or a module that
	// moved. 1s if zero.
	Poll time.Duration
	// Settle is how long go.mod and go.sum must stay unchanged before
	// gopls is started in their module. DefaultSettle if zero.
	Settle time.Duration
	// RestartWait bounds how long a restart waits for the requests in
	// flight to be answered. 30s if zero.
	RestartWait time.Duration
	// ReplayTimeout bounds the wait for the new child's reply to a
	// replayed request. 30s if zero.
	ReplayTimeout time.Duration
	// Log, if set, gets a line for every start, restart and trouble.
	Log io.Writer

	// ticks and now, when a test sets them, replace the Poll ticker and
	// the clock Settle is measured on.
	ticks <-chan time.Time
	now   func() time.Time
}

type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		RequestID json.RawMessage `json:"requestId"`
	} `json:"params"`
}

func key(id json.RawMessage) string { return string(bytes.TrimSpace(id)) }

// isRequest is a message that expects a reply; isReply answers one.
func (m message) isRequest() bool { return m.ID != nil && m.Method != "" }
func (m message) isReply() bool   { return m.ID != nil && m.Method == "" }

// running is one child and the generation it belongs to.
type running struct {
	child      Child
	generation int
	writing    sync.Mutex
}

func (r *running) write(line []byte) error {
	r.writing.Lock()
	defer r.writing.Unlock()
	_, err := r.child.In.Write(line)
	return err
}

type proxy struct {
	*Proxy
	out     io.Writer
	writing sync.Mutex // out

	// gate is held for reading while a client message is forwarded, and
	// for writing while a child is swapped in, so no message reaches a
	// child that is going away.
	gate sync.RWMutex

	mutex   sync.Mutex
	changed *sync.Cond // inFlight shrank
	current *running
	// joining is a new child being given the session; only its replies
	// to the replay are read until it becomes current.
	joining *running
	// inFlight are client requests the current child has not answered.
	inFlight map[string]bool
	// session is what the client sent to set up its session, in order:
	// initialize, notifications/initialized and subscriptions/listen.
	session [][]byte
	// listening are open subscriptions, never in flight.
	listening map[string]bool
	// replayReply is closed when the joining child answers the replayed
	// request with that id.
	replayReply map[string]chan struct{}
	// replayAcknowledgements counts subscription acknowledgements the
	// joining child will send for replayed subscriptions.
	replayAcknowledgements int

	childDied chan error
}

// Run forwards between the client and the child until the client's
// input ends or the child dies.
func (p *Proxy) Run(ctx context.Context, client io.Reader, out io.Writer) error {
	x := &proxy{Proxy: p, out: out, inFlight: map[string]bool{},
		listening: map[string]bool{}, replayReply: map[string]chan struct{}{},
		childDied: make(chan error, 1)}
	x.changed = sync.NewCond(&x.mutex)

	module := ModuleDir(p.Dir)
	start := p.Dir
	if module != "" {
		start = module
	}
	first, err := x.launch(start, 1)
	if err != nil {
		x.trouble("gopls did not start in %s: %v", start, err)
		return err
	}
	x.current = first
	x.logf("started gopls in %s", start)
	if module != "" {
		x.logf("module active: gopls 1 in %s", module)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go x.followModule(ctx, module)

	clientDone := make(chan error, 1)
	go func() { clientDone <- x.forwardClient(client) }()
	select {
	case err = <-clientDone:
	case err = <-x.childDied:
	case <-ctx.Done():
		err = ctx.Err()
	}
	cancel()
	x.mutex.Lock()
	x.changed.Broadcast() // a restart waiting on in-flight requests
	x.mutex.Unlock()
	x.gate.Lock()
	x.mutex.Lock()
	last := x.current
	x.current = nil
	x.mutex.Unlock()
	x.gate.Unlock()
	if last != nil {
		last.child.Stop()
	}
	return err
}

func (x *proxy) logf(format string, args ...any) {
	if x.Log != nil {
		fmt.Fprintf(x.Log, "%s goplsmcp: %s\n",
			time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	}
}

func (x *proxy) trouble(format string, args ...any) {
	if x.Log != nil {
		fmt.Fprintf(x.Log, "%s %s%s\n", time.Now().UTC().Format(time.RFC3339),
			TroubleMarker, fmt.Sprintf(format, args...))
	}
}

func (x *proxy) launch(dir string, generation int) (*running, error) {
	child, err := x.Start(dir)
	if err != nil {
		return nil, err
	}
	r := &running{child: child, generation: generation}
	go x.forwardChild(r)
	return r, nil
}

// forwardClient sends each client line to the current child, noting
// what a later child must be given and what is in flight.
func (x *proxy) forwardClient(client io.Reader) error {
	lines := bufio.NewReader(client)
	for {
		line, err := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if line[len(line)-1] != '\n' {
				line = append(line, '\n')
			}
			if writeErr := x.toChild(line); writeErr != nil {
				return writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (x *proxy) toChild(line []byte) error {
	x.gate.RLock()
	defer x.gate.RUnlock()
	var m message
	json.Unmarshal(line, &m)
	x.mutex.Lock()
	switch m.Method {
	case "initialize", "notifications/initialized":
		x.session = append(x.session, line)
	case "subscriptions/listen":
		x.session = append(x.session, line)
		x.listening[key(m.ID)] = true
	case "notifications/cancelled":
		// A cancelled request may never be answered.
		delete(x.inFlight, key(m.Params.RequestID))
		x.changed.Broadcast()
	}
	if m.isRequest() && !x.listening[key(m.ID)] {
		x.inFlight[key(m.ID)] = true
	}
	target := x.current
	x.mutex.Unlock()
	if target == nil {
		return errors.New("no gopls is running")
	}
	return target.write(line)
}

// forwardChild sends a child's lines to the client while it is the
// current child.
func (x *proxy) forwardChild(r *running) {
	lines := bufio.NewReader(r.child.Out)
	for {
		line, err := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 && x.fromChild(r, line) {
			x.writing.Lock()
			x.out.Write(line)
			x.writing.Unlock()
		}
		if err != nil {
			if closer, ok := r.child.Out.(io.Closer); ok {
				closer.Close()
			}
			x.mutex.Lock()
			died := x.current == r
			x.mutex.Unlock()
			if died {
				x.trouble("gopls %d exited mid-session: %v", r.generation, err)
				select {
				case x.childDied <- fmt.Errorf("gopls exited: %v", err):
				default:
				}
			}
			return
		}
	}
}

// fromChild says whether a child's line goes on to the client.
func (x *proxy) fromChild(r *running, line []byte) bool {
	var m message
	json.Unmarshal(line, &m)
	x.mutex.Lock()
	defer x.mutex.Unlock()
	if x.joining == r {
		// Its replies to the replay were had from the first child.
		if done, replayed := x.replayReply[key(m.ID)]; replayed && m.isReply() {
			delete(x.replayReply, key(m.ID))
			close(done)
		}
		if m.Method == "notifications/subscriptions/acknowledged" &&
			x.replayAcknowledgements > 0 {
			x.replayAcknowledgements--
		}
		return false
	}
	if x.current != r {
		return false
	}
	if m.isReply() && x.inFlight[key(m.ID)] {
		delete(x.inFlight, key(m.ID))
		x.changed.Broadcast()
	}
	// A replayed subscription is acknowledged whenever the child gets
	// to it, which may be after it took over.
	if m.Method == "notifications/subscriptions/acknowledged" &&
		x.replayAcknowledgements > 0 {
		x.replayAcknowledgements--
		return false
	}
	return true
}

// followModule starts gopls again whenever the app's module is not the
// one gopls runs in (it appeared, or the agent moved it), once its go.mod
// and go.sum have not changed for Settle. active is gopls's module, or
// "" while it has none.
func (x *proxy) followModule(ctx context.Context, active string) {
	settle, poll := x.Settle, x.Poll
	if settle == 0 {
		settle = DefaultSettle
	}
	if poll == 0 {
		poll = time.Second
	}
	ticks, now := x.ticks, x.now
	if ticks == nil {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		ticks = ticker.C
	}
	if now == nil {
		now = time.Now
	}
	seen, since := "", time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		module := ModuleDir(x.Dir)
		if module == "" || module == active {
			seen = ""
			continue
		}
		state := module + "\x00" + moduleState(module)
		if state != seen {
			seen, since = state, now()
			continue
		}
		if now().Sub(since) < settle {
			continue
		}
		if err := x.restart(ctx, module); err != nil {
			x.trouble("gopls was not restarted in %s, so the session "+
				"keeps the gopls that cannot see it: %v", module, err)
			return
		}
		active, seen = module, ""
	}
}

// moduleState changes whenever go.mod or go.sum is written.
func moduleState(dir string) string {
	var state strings.Builder
	for _, name := range []string{"go.mod", "go.sum"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil {
			fmt.Fprintf(&state, "%s %d %d;", name, info.Size(),
				info.ModTime().UnixNano())
		}
	}
	return state.String()
}

// restart waits until nothing is in flight, holding new client messages
// back, then starts a child in module and gives it the session. Only a
// child that took the whole session replaces the running one; if
// anything fails, the running one stays. A request still unanswered
// after RestartWait, or a session that ends first, is a failure too:
// gopls would otherwise never see the module.
func (x *proxy) restart(ctx context.Context, module string) error {
	x.logf("found %s; restarting gopls once nothing is in flight",
		filepath.Join(module, "go.mod"))
	wait := x.RestartWait
	if wait == 0 {
		wait = 30 * time.Second
	}
	expired := false
	timer := time.AfterFunc(wait, func() {
		x.mutex.Lock()
		expired = true
		x.changed.Broadcast()
		x.mutex.Unlock()
	})
	defer timer.Stop()
	for {
		x.gate.Lock()
		x.mutex.Lock()
		if len(x.inFlight) == 0 {
			break
		}
		x.mutex.Unlock()
		x.gate.Unlock()
		x.mutex.Lock()
		for len(x.inFlight) > 0 && ctx.Err() == nil && !expired {
			x.changed.Wait()
		}
		inFlight, timedOut := len(x.inFlight), expired
		x.mutex.Unlock()
		switch {
		case ctx.Err() != nil:
			return errors.New("the session ended with a request in flight")
		case timedOut && inFlight > 0:
			return fmt.Errorf("%d requests were still in flight after %s",
				inFlight, wait)
		}
	}
	defer x.gate.Unlock()
	old := x.current
	session := x.session
	x.mutex.Unlock()
	if old == nil {
		return errors.New("the session ended")
	}

	fresh, err := x.launch(module, old.generation+1)
	if err != nil {
		return err
	}
	x.mutex.Lock()
	x.joining = fresh
	x.mutex.Unlock()
	if err := x.replay(fresh, session); err != nil {
		x.mutex.Lock()
		x.joining = nil
		x.replayReply = map[string]chan struct{}{}
		x.replayAcknowledgements = 0
		x.mutex.Unlock()
		fresh.child.Stop()
		return err
	}
	x.mutex.Lock()
	x.joining = nil
	x.current = fresh
	x.mutex.Unlock()
	old.child.Stop()
	x.logf("restarted gopls in %s", module)
	x.logf("module active: gopls %d in %s", fresh.generation, module)
	return nil
}

// replay gives a joining child the client's session messages, waiting
// for its reply to each request.
func (x *proxy) replay(fresh *running, session [][]byte) error {
	timeout := x.ReplayTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	for _, line := range session {
		var m message
		json.Unmarshal(line, &m)
		var reply chan struct{}
		x.mutex.Lock()
		switch {
		case m.Method == "subscriptions/listen":
			x.replayAcknowledgements++
		case m.isRequest():
			reply = make(chan struct{})
			x.replayReply[key(m.ID)] = reply
		}
		x.mutex.Unlock()
		if err := fresh.write(line); err != nil {
			return err
		}
		if reply == nil {
			continue
		}
		select {
		case <-reply:
		case <-time.After(timeout):
			return fmt.Errorf("gopls did not answer the replayed %s "+
				"within %s", m.Method, timeout)
		}
	}
	return nil
}

// ModuleDir is the directory of the app's go.mod: dir's own, else the
// first found up to three levels below, outside hidden, vendored and
// test-data directories. "" when there is none.
func ModuleDir(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return dir
	}
	found := ""
	filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			name := entry.Name()
			depth := strings.Count(strings.TrimPrefix(path, dir), string(filepath.Separator))
			if path != dir && (strings.HasPrefix(name, ".") || name == "vendor" ||
				name == "testdata" || name == "node_modules" || depth > 3) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "go.mod" {
			found = filepath.Dir(path)
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
