//go:build browser

package harness

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Listen calls handle with every event of this kind that the browser
// sends to the tab of ctx, in the order it sent them, before the reply
// to any later command reaches the drive. So once a command returns,
// every event sent before its reply has been handled, and a drive can
// check that something did NOT happen without waiting for it.
//
// Listening starts once the tab is open, so events from opening it are
// not seen. handle runs on chromedp's reader, so keep it short, never
// send a command from it, and never hold a lock it takes while you send
// one: the reply could not arrive. The listening ends when ctx does, or
// at stop; once stop returns, handle is not running and will not run.
// Do not call stop from handle.
func Listen[E any](
	rig *Rig, ctx context.Context, event cdp.Event[E], handle func(*E),
) (stop func()) {
	rig.t.Helper()
	if err := chromedp.Do(ctx); err != nil {
		rig.t.Fatalf("harness: opening the tab to listen to: %v", err)
	}
	listener := &tapListener{
		ctx:     ctx,
		session: chromedp.FromContext(ctx).Target.SessionID,
		method:  event.Method,
		quoted:  []byte(strconv.Quote(event.Method)),
		handle: func(params jsonv2.Value) {
			var payload E
			err := jsonv2.Unmarshal(params, &payload,
				chromedp.DefaultUnmarshalOptions)
			if err != nil {
				rig.add(fmt.Sprintf("harness: decoding %s: %v",
					event.Method, err))
				return
			}
			handle(&payload)
		},
	}
	rig.tap.add(listener)
	stop = func() {
		rig.tap.remove(listener)
		listener.mu.Lock()
		defer listener.mu.Unlock()
		listener.stopped = true
	}
	context.AfterFunc(ctx, stop)
	return stop
}

// protocolTap reads every message the browser sends, on chromedp's own
// reader and before chromedp decodes it. chromedp v0.21 hands events to
// their subscribers on goroutines of their own, which can run after the
// reply to the next command; read here, an event is handled before that
// reply reaches the drive, as v0.14's ListenTarget did. The hook is
// chromedp's debug log and its "<- %s" line, so a change to that line
// would leave the watchers deaf, and the harness's own tests fail.
type protocolTap struct {
	mu        sync.Mutex
	listeners []*tapListener
}

type tapListener struct {
	ctx     context.Context
	session target.SessionID
	method  string
	quoted  []byte
	handle  func(params jsonv2.Value)

	// mu is held while handle runs, so stop can wait out a call that
	// a dispatch already under way has begun.
	mu      sync.Mutex
	stopped bool
}

func (listener *tapListener) deliver(params jsonv2.Value) {
	listener.mu.Lock()
	defer listener.mu.Unlock()
	if !listener.stopped && listener.ctx.Err() == nil {
		listener.handle(params)
	}
}

func (tap *protocolTap) add(listener *tapListener) {
	tap.mu.Lock()
	defer tap.mu.Unlock()
	tap.listeners = append(tap.listeners, listener)
}

func (tap *protocolTap) remove(listener *tapListener) {
	tap.mu.Lock()
	defer tap.mu.Unlock()
	tap.listeners = slices.DeleteFunc(tap.listeners,
		func(each *tapListener) bool { return each == listener })
}

// read is the debug log function chromedp calls for every message.
func (tap *protocolTap) read(format string, arguments ...any) {
	if format != "<- %s" || len(arguments) != 1 {
		return
	}
	raw, isBytes := arguments[0].([]byte)
	if !isBytes {
		return
	}
	interested := tap.interestedIn(raw)
	if len(interested) == 0 {
		return
	}
	var message cdproto.Message
	err := jsonv2.Unmarshal(raw, &message, chromedp.DefaultUnmarshalOptions)
	if err != nil {
		return
	}
	for _, listener := range interested {
		if listener.method == string(message.Method) &&
			listener.session == message.SessionID {
			listener.deliver(message.Params)
		}
	}
}

// interestedIn is the listeners whose method the raw message mentions:
// a cheap test that spares decoding the screenshots and traces nobody
// is listening for.
func (tap *protocolTap) interestedIn(raw []byte) []*tapListener {
	tap.mu.Lock()
	defer tap.mu.Unlock()
	var interested []*tapListener
	for _, listener := range tap.listeners {
		if bytes.Contains(raw, listener.quoted) {
			interested = append(interested, listener)
		}
	}
	return interested
}
