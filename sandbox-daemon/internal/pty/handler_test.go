package pty

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
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

// --- pty.output_dropped (Phase 6 polish) ------------------------------------

// TestHandler_OutputDroppedSurfaced verifies the per-session drop counter
// surfaces a pty.output_dropped event on the next successful publish after
// the pump had dropped frames.
//
// Setup: fakePublisher.dropAll flips Publish to a no-op that returns false,
// the same signal connPublisher returns when the pump's drop-oldest path
// gives up. Drive output, observe drops, flip dropAll back, drive more
// output → the next pty.output is preceded by exactly one pty.output_dropped
// with dropped_count > 0.
func TestHandler_OutputDroppedSurfaced(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	// Start in drop-everything mode so the shell's startup output is dropped.
	pub.dropAll.Store(true)

	id := mustOpen(t, h, pub, "c", 80, 24)

	// Drive an initial output burst that the pub drops.
	driveIn := func(line string) {
		t.Helper()
		req, _ := json.Marshal(map[string]any{
			"pty_id": id,
			"data":   base64.StdEncoding.EncodeToString([]byte(line)),
		})
		if _, errBody := h.Input(ws.HandlerCtx{ConnID: "c", Publisher: pub}, req); errBody != nil {
			t.Fatalf("input: %+v", errBody)
		}
	}
	driveIn("printf 'first-batch\\n'\n")

	// Give the outputLoop time to read + try-publish the dropped frames.
	time.Sleep(200 * time.Millisecond)

	// Flip the publisher back on. The next read in outputLoop will see
	// droppedSinceFlush > 0 and emit pty.output_dropped before pty.output.
	pub.dropAll.Store(false)
	driveIn("printf 'second-batch\\n'\nexit 0\n")
	_ = pub.waitFor(t, "pty.exit", 5*time.Second)

	var sawDropped bool
	var sawDroppedBeforeOutput bool
	for _, e := range pub.snapshot() {
		switch e.typ {
		case "pty.output_dropped":
			sawDropped = true
			var od struct {
				PtyID        string `json:"pty_id"`
				DroppedCount int    `json:"dropped_count"`
			}
			if json.Unmarshal(e.payload, &od) != nil || od.DroppedCount <= 0 {
				t.Fatalf("output_dropped malformed or count<=0: %s", string(e.payload))
			}
			if od.PtyID != id {
				t.Fatalf("output_dropped pty_id: got %q want %q", od.PtyID, id)
			}
		case "pty.output":
			if sawDropped {
				sawDroppedBeforeOutput = true
			}
		}
	}
	if !sawDropped {
		t.Fatalf("never saw pty.output_dropped after re-enabling publisher")
	}
	if !sawDroppedBeforeOutput {
		t.Fatalf("pty.output_dropped emitted but not before any pty.output — ordering invariant broken")
	}
}

// --- pty.start_agent (Phase 3) ----------------------------------------------

func TestStartAgent_UnknownAgent(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	req, _ := json.Marshal(map[string]any{"agent": "not-an-agent"})
	_, errBody := h.StartAgent(ws.HandlerCtx{ConnID: "c", Publisher: pub}, req)
	if errBody == nil {
		t.Fatalf("expected error")
	}
	// Codegen rejects out-of-enum values with bad_request; if the enum is
	// loosened in the future the handler-level pty.unknown_agent kicks in.
	if errBody.Code != ws.ErrCodePtyUnknownAgent && errBody.Code != ws.ErrCodeBadRequest {
		t.Fatalf("code: got %+v want pty.unknown_agent or bad_request", errBody)
	}
}

func TestStartAgent_SharedSoftCap(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	for i := 0; i < MaxPTYsPerConn; i++ {
		mustOpen(t, h, pub, "connA", 80, 24)
	}
	// Stub a claude binary so we'd actually attempt to exec it if the cap
	// check were missing — that way a regression that drops the cap check
	// shows up as "spawned a 5th process" rather than "the test passes by
	// accident because exec failed".
	stubAgent(t, "claude", `#!/bin/sh
exit 0
`)
	req, _ := json.Marshal(map[string]any{"agent": "claude"})
	_, errBody := h.StartAgent(ws.HandlerCtx{ConnID: "connA", Publisher: pub}, req)
	if errBody == nil || errBody.Code != ws.ErrCodePtyLimitReached {
		t.Fatalf("expected pty.limit_reached, got %+v", errBody)
	}
}

func TestStartAgent_HappyPath(t *testing.T) {
	h := New(t.TempDir())
	pub := &fakePublisher{}
	stubAgent(t, "claude", `#!/bin/sh
printf 'agent-up\n'
exit 0
`)
	req, _ := json.Marshal(map[string]any{"agent": "claude"})
	resp, errBody := h.StartAgent(ws.HandlerCtx{ConnID: "c", Publisher: pub}, req)
	if errBody != nil {
		t.Fatalf("start_agent: %+v", errBody)
	}
	var out struct {
		PtyID string `json:"pty_id"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.PtyID == "" {
		t.Fatalf("empty pty_id")
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
	if !strings.Contains(combined.String(), "agent-up") {
		t.Fatalf("agent output never reached publisher; saw %q", combined.String())
	}
}

func TestStartAgent_CwdAndEnvPassthrough(t *testing.T) {
	root := t.TempDir()
	h := New(root)
	pub := &fakePublisher{}

	// Stub agent prints the cwd path and one env var so the test can grep both.
	stubAgent(t, "claude", `#!/bin/sh
printf 'CWD=%s ROMMEL_TEST_VAR=%s\n' "$(pwd)" "$ROMMEL_TEST_VAR"
exit 0
`)

	// Pre-create the sub-workspace dir so the cwd sandbox succeeds.
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	req, _ := json.Marshal(map[string]any{
		"agent": "claude",
		"cwd":   "subdir",
		"env":   map[string]string{"ROMMEL_TEST_VAR": "hello-rommel"},
	})
	_, errBody := h.StartAgent(ws.HandlerCtx{ConnID: "c", Publisher: pub}, req)
	if errBody != nil {
		t.Fatalf("start_agent: %+v", errBody)
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
		if b, err := base64.StdEncoding.DecodeString(oe.Data); err == nil {
			combined.Write(b)
		}
	}
	out := combined.String()
	if !strings.Contains(out, "ROMMEL_TEST_VAR=hello-rommel") {
		t.Fatalf("env not propagated: %q", out)
	}
	if !strings.Contains(out, "subdir") {
		t.Fatalf("cwd not honored (no /subdir in output): %q", out)
	}
}

// stubAgent writes a tiny shell script named `name` into a t.TempDir() and
// prepends that dir onto $PATH for the lifetime of the test. Used by the
// pty.start_agent tests so we can prove the handler execs the named binary
// without requiring the real agent CLI to be installed.
func stubAgent(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
