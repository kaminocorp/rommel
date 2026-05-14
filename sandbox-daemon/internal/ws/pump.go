package ws

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// writePump owns the write side of a single WebSocket connection.
//
// gorilla/websocket panics on concurrent writes. Phases 1–6 sidestepped this
// because the per-connection read loop in runConn was the only writer. Phase 7
// adds server-pushed events (pty.output) that originate on goroutines other
// than the read loop, so all outbound frames now funnel through this pump:
//
//   - Send(f)       blocking submit. Used for request responses / errors that
//                   must reach the client (correctness-critical).
//   - Publish(f)    best-effort submit. Used for server-pushed events
//                   (pty.output, pty.exit, …). If the buffer is full, the
//                   pump drops the *oldest* enqueued frame to make room.
//                   Returns false if the new frame was itself dropped
//                   (degenerate case: pump shutting down or back-to-back full).
//
// The drop-oldest policy is deliberate: when an event firehose (cat
// /dev/urandom | base64) outpaces the browser's render loop, newer output is
// more relevant to the user than stale output. dropFn lets the handler that
// authored the dropped frame surface the loss to the client (pty handler
// emits pty.output_dropped with a count).
type writePump struct {
	conn      *websocket.Conn
	out       chan *Frame
	done      chan struct{}
	closeOnce sync.Once
	dropFn    func(*Frame)
}

// PumpBufferSize is the per-connection write buffer (frames, not bytes). At
// ~4 KiB/frame this is ~1 MiB of in-flight tolerance — enough to ride out
// frontend GC pauses, not enough to OOM the daemon on a runaway process.
const PumpBufferSize = 256

// writeDeadline caps a single WriteJSON call. Slow client / dead socket bails
// fast rather than wedging the pump goroutine.
const writeDeadline = 5 * time.Second

func startPump(conn *websocket.Conn, dropFn func(*Frame)) *writePump {
	p := &writePump{
		conn:   conn,
		out:    make(chan *Frame, PumpBufferSize),
		done:   make(chan struct{}),
		dropFn: dropFn,
	}
	go p.loop()
	return p
}

// loop drains out → socket. Exits when out is closed (graceful shutdown) or
// the underlying socket returns a write error (socket already dead).
func (p *writePump) loop() {
	for f := range p.out {
		_ = p.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
		if err := p.conn.WriteJSON(f); err != nil {
			log.Printf("ws: write error: %v", err)
			p.close()
			// Drain remaining frames so senders unblock and dropFn fires for
			// what wasn't delivered.
			for f := range p.out {
				if p.dropFn != nil {
					p.dropFn(f)
				}
			}
			return
		}
	}
}

// Send blocks until the frame is enqueued or the pump is closed. Reserved for
// responses/errors — losing one of those is a protocol break.
func (p *writePump) Send(f *Frame) {
	select {
	case p.out <- f:
	case <-p.done:
		if p.dropFn != nil {
			p.dropFn(f)
		}
	}
}

// Publish enqueues a frame best-effort. If the buffer is full, it drops the
// oldest enqueued frame and retries once; if still full (rare race), drops
// the new frame. Returns true if the new frame made it into the buffer.
func (p *writePump) Publish(f *Frame) bool {
	select {
	case <-p.done:
		if p.dropFn != nil {
			p.dropFn(f)
		}
		return false
	default:
	}
	select {
	case p.out <- f:
		return true
	default:
	}
	// Buffer full — drop one to make room, then retry once.
	select {
	case dropped := <-p.out:
		if p.dropFn != nil {
			p.dropFn(dropped)
		}
	default:
	}
	select {
	case p.out <- f:
		return true
	case <-p.done:
		if p.dropFn != nil {
			p.dropFn(f)
		}
		return false
	default:
		if p.dropFn != nil {
			p.dropFn(f)
		}
		return false
	}
}

// close signals the loop to terminate. Idempotent.
func (p *writePump) close() {
	p.closeOnce.Do(func() {
		close(p.done)
		close(p.out)
	})
}

// connPublisher adapts a writePump to the Publisher interface. The wrapped
// pump knows the connection; the publisher knows the envelope shape.
type connPublisher struct {
	pump *writePump
}

func (cp *connPublisher) Publish(eventType string, payload []byte) bool {
	frame := &Frame{
		Kind:    eventKind,
		Type:    eventType,
		Payload: payload,
	}
	return cp.pump.Publish(frame)
}
