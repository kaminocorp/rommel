package pty

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

// fakePublisher captures Publish calls so tests can assert event types,
// payloads, and ordering. dropAll, if set, drops every Publish and records
// it as a "drop" rather than an event — useful to exercise the
// pty.output_dropped emission path.
type fakePublisher struct {
	mu      sync.Mutex
	events  []recorded
	dropAll atomic.Bool
}

type recorded struct {
	typ     string
	payload []byte
}

func (p *fakePublisher) Publish(eventType string, payload []byte) bool {
	if p.dropAll.Load() {
		// Caller treats false as "dropped"; we still record nothing — the
		// pty handler's accounting (droppedSinceFlush) is what we're
		// observing through the next successful Publish.
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	dup := make([]byte, len(payload))
	copy(dup, payload)
	p.events = append(p.events, recorded{typ: eventType, payload: dup})
	return true
}

func (p *fakePublisher) snapshot() []recorded {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]recorded, len(p.events))
	copy(out, p.events)
	return out
}

func (p *fakePublisher) waitFor(t *testing.T, typ string, timeout time.Duration) recorded {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, e := range p.snapshot() {
			if e.typ == typ {
				return e
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never saw event %q within %s; saw %d events", typ, timeout, len(p.snapshot()))
	return recorded{}
}

func mustOpen(t *testing.T, h *Handler, pub ws.Publisher, connID string, cols, rows int) string {
	t.Helper()
	req, _ := json.Marshal(map[string]any{"cols": cols, "rows": rows})
	resp, errBody := h.Open(ws.HandlerCtx{ConnID: connID, Publisher: pub}, req)
	if errBody != nil {
		t.Fatalf("open: %+v", errBody)
	}
	var out struct {
		PtyID string `json:"pty_id"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out.PtyID
}

func TestHandler_SoftCap(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	for i := 0; i < MaxPTYsPerConn; i++ {
		mustOpen(t, h, pub, "connA", 80, 24)
	}
	// (MaxPTYsPerConn + 1)th open from the same conn should fail.
	req, _ := json.Marshal(map[string]any{"cols": 80, "rows": 24})
	_, errBody := h.Open(ws.HandlerCtx{ConnID: "connA", Publisher: pub}, req)
	if errBody == nil || errBody.Code != ws.ErrCodePtyLimitReached {
		t.Fatalf("expected pty.limit_reached, got %+v", errBody)
	}
	// Different connection — independent quota.
	_, errBody = h.Open(ws.HandlerCtx{ConnID: "connB", Publisher: pub}, req)
	if errBody != nil {
		t.Fatalf("connB open: %+v", errBody)
	}
}

func TestHandler_OpenInvalidSize(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	// Manually craft a payload that bypasses the codegen unmarshaler's
	// validation by hitting the handler's own bound check.
	req, _ := json.Marshal(map[string]any{"cols": 0, "rows": 24})
	_, errBody := h.Open(ws.HandlerCtx{ConnID: "c", Publisher: pub}, req)
	if errBody == nil {
		t.Fatalf("expected error")
	}
	// codegen rejects with bad_request before the handler's invalid_size
	// check runs; either is acceptable — both prove a 0×N PTY can't be
	// spawned.
	if errBody.Code != ws.ErrCodePtyInvalidSize && errBody.Code != ws.ErrCodeBadRequest {
		t.Fatalf("code: got %+v", errBody)
	}
}

func TestHandler_BadCwd(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	req, _ := json.Marshal(map[string]any{"cols": 80, "rows": 24, "cwd": "../../etc"})
	_, errBody := h.Open(ws.HandlerCtx{ConnID: "c", Publisher: pub}, req)
	if errBody == nil || errBody.Code != ws.ErrCodePtySpawnFailed {
		t.Fatalf("expected pty.spawn_failed, got %+v", errBody)
	}
}

func TestHandler_InputAndExit(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	id := mustOpen(t, h, pub, "c", 80, 24)

	// Drive the shell to exit with a specific status.
	inReq, _ := json.Marshal(map[string]any{
		"pty_id": id,
		"data":   base64.StdEncoding.EncodeToString([]byte("exit 7\n")),
	})
	if _, errBody := h.Input(ws.HandlerCtx{ConnID: "c", Publisher: pub}, inReq); errBody != nil {
		t.Fatalf("input: %+v", errBody)
	}
	ev := pub.waitFor(t, "pty.exit", 5*time.Second)
	var exit struct {
		PtyID    string  `json:"pty_id"`
		ExitCode int     `json:"exit_code"`
		Signal   *string `json:"signal,omitempty"`
	}
	if err := json.Unmarshal(ev.payload, &exit); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if exit.ExitCode != 7 {
		t.Fatalf("exit_code: got %d want 7", exit.ExitCode)
	}
	if exit.PtyID != id {
		t.Fatalf("pty_id: got %q want %q", exit.PtyID, id)
	}
}

func TestHandler_OutputContainsEcho(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	id := mustOpen(t, h, pub, "c", 80, 24)

	inReq, _ := json.Marshal(map[string]any{
		"pty_id": id,
		"data":   base64.StdEncoding.EncodeToString([]byte("printf 'rommel-handler\\n'\nexit 0\n")),
	})
	if _, errBody := h.Input(ws.HandlerCtx{ConnID: "c", Publisher: pub}, inReq); errBody != nil {
		t.Fatalf("input: %+v", errBody)
	}
	_ = pub.waitFor(t, "pty.exit", 5*time.Second)

	var combined strings.Builder
	for _, e := range pub.snapshot() {
		if e.typ != "pty.output" {
			continue
		}
		var oe struct {
			Data string `json:"data"`
		}
		if json.Unmarshal(e.payload, &oe) != nil {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(oe.Data)
		if err == nil {
			combined.Write(b)
		}
	}
	if !strings.Contains(combined.String(), "rommel-handler") {
		t.Fatalf("output never contained marker; got %q", combined.String())
	}
}

func TestHandler_CloseIsIdempotent(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	id := mustOpen(t, h, pub, "c", 80, 24)

	closeReq, _ := json.Marshal(map[string]any{"pty_id": id})
	resp, errBody := h.Close(ws.HandlerCtx{ConnID: "c", Publisher: pub}, closeReq)
	if errBody != nil {
		t.Fatalf("close 1: %+v", errBody)
	}
	if string(resp) != `{}` {
		t.Fatalf("close payload: %q", resp)
	}
	// Second close — same id, already gone — still success.
	if _, errBody := h.Close(ws.HandlerCtx{ConnID: "c", Publisher: pub}, closeReq); errBody != nil {
		t.Fatalf("close 2: %+v", errBody)
	}
	// Unknown id — also success.
	unknown, _ := json.Marshal(map[string]any{"pty_id": "ghost"})
	if _, errBody := h.Close(ws.HandlerCtx{ConnID: "c", Publisher: pub}, unknown); errBody != nil {
		t.Fatalf("close unknown: %+v", errBody)
	}
}

func TestHandler_OnDisconnectSparesOtherConns(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	mustOpen(t, h, pub, "connA", 80, 24)
	mustOpen(t, h, pub, "connA", 80, 24)
	mustOpen(t, h, pub, "connB", 80, 24)

	h.OnDisconnect("connA")

	// Wait for waitLoop goroutines to finish unregister.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.OpenSessionCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.OpenSessionCount(); got != 1 {
		t.Fatalf("after OnDisconnect: %d sessions left, want 1", got)
	}
}
