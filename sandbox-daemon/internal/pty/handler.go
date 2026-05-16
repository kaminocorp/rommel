// Package pty implements the pty.* primitives: pty.open / pty.input /
// pty.resize / pty.close as request/response RPCs, and pty.output / pty.exit
// / pty.output_dropped as server-pushed events.
//
// Each session pairs a child shell process with the PTY master file. The
// handler owns a daemon-global map of sessions keyed by pty_id, each tagged
// with the connection that opened it. When a connection drops, OnDisconnect
// walks the map and SIGTERMs every session owned by that connection (200 ms
// grace, then SIGKILL on stragglers).
//
// Output coalescing: a per-session read goroutine pulls up to 4 KiB at a
// time from the PTY master, base64-encodes the bytes, and Publishes one
// pty.output event per read. The kernel already coalesces small writes
// across the slave fd, so a 5 ms flush timer would be redundant in practice.
//
// Backpressure: the per-connection write pump applies drop-oldest to events
// (see internal/ws/pump.go). When the publish goroutine sees a drop it
// increments a per-session counter; on the next successful publish it
// prepends a pty.output_dropped event so the FE can flash "output truncated".
//
// Soft cap of 4 PTYs per connection — defence against runaway-agent bugs.
// The frontend in v1 shows exactly one terminal, so any caller hitting this
// cap is misbehaving.
package pty

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

const (
	// MaxPTYsPerConn caps the number of concurrent PTYs one WS connection can
	// own. The frontend shows one terminal in v1; anything beyond a small
	// multiple is a bug.
	MaxPTYsPerConn = 4

	// readBufSize bounds one pty.output frame's payload in bytes (before
	// base64 — the wire payload is ~4/3× this).
	readBufSize = 4 << 10 // 4 KiB

	// closeGrace is how long the disconnect cleanup waits between SIGTERM
	// and SIGKILL. Plenty of time for an interactive shell to flush its
	// scrollback and exit cleanly.
	closeGrace = 200 * time.Millisecond
)

// Handler owns the daemon-global PTY table. One instance for the whole
// process — sessions are keyed by pty_id (UUID) and tagged with the
// connection id that opened them.
type Handler struct {
	WorkspaceRoot string

	mu       sync.Mutex
	sessions map[string]*session // keyed by pty_id
}

// New returns a Handler with the workspace root pinned. Sessions cannot
// escape this root (PtyOpenRequest.cwd is sandboxed against it the same way
// fs.* paths are).
func New(workspaceRoot string) *Handler {
	return &Handler{
		WorkspaceRoot: workspaceRoot,
		sessions:      make(map[string]*session),
	}
}

type session struct {
	id     string
	connID string
	cmd    *exec.Cmd
	file   *os.File // PTY master fd

	// closing flips once when Close / OnDisconnect / shell-exit start
	// teardown. Subsequent close calls are no-ops (pty.close is idempotent).
	closing atomic.Bool

	// done is closed after pty.exit has been published. Tests use this as
	// the synchronisation point; OnDisconnect blocks on it for the grace
	// window so SIGKILL fires after the process has had a fair shot.
	done chan struct{}
}

// Open implements pty.open.
func (h *Handler) Open(hc ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.PtyOpenRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "pty.open: invalid payload: "+err.Error())
	}
	if req.Cols < 1 || req.Cols > 1000 || req.Rows < 1 || req.Rows > 1000 {
		return nil, errBody(ws.ErrCodePtyInvalidSize, "pty.open: cols/rows out of bounds")
	}

	if n := h.countByConn(hc.ConnID); n >= MaxPTYsPerConn {
		return nil, errBody(ws.ErrCodePtyLimitReached,
			fmt.Sprintf("pty.open: connection already owns %d PTYs (cap %d)", n, MaxPTYsPerConn))
	}

	cwd := h.WorkspaceRoot
	if req.Cwd != nil && *req.Cwd != "" {
		resolved, err := h.resolveCwd(*req.Cwd)
		if err != nil {
			return nil, errBody(ws.ErrCodePtySpawnFailed, "pty.open: "+err.Error())
		}
		cwd = resolved
	}

	shell := pickShell()
	cmd := exec.Command(shell)
	cmd.Dir = cwd
	cmd.Env = mergeEnv(map[string]string(req.Env))
	// Become a session leader so we can SIGTERM the whole process group on
	// teardown (signals to the leader propagate to descendants).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	file, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(req.Rows),
		Cols: uint16(req.Cols),
	})
	if err != nil {
		return nil, errBody(ws.ErrCodePtySpawnFailed, "pty.open: "+err.Error())
	}

	s := &session{
		id:     uuid.NewString(),
		connID: hc.ConnID,
		cmd:    cmd,
		file:   file,
		done:   make(chan struct{}),
	}
	h.register(s)

	publisher := hc.Publisher
	go h.outputLoop(s, publisher)
	go h.waitLoop(s, publisher)

	resp, err := json.Marshal(protogen.PtyOpenResponse{PtyID: s.id})
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "pty.open: marshal: "+err.Error())
	}
	return resp, nil
}

// StartAgent implements pty.start_agent (Phase 3).
// It allocates a PTY exactly like pty.open but execs a known agent CLI
// (claude / codex / cursor) instead of a shell. All subsequent pty.*
// operations (input, output events, resize, close, exit) work identically.
// The agent binary must be present in $PATH inside the workspace image
// (user can `npm i -g ...` or apt install beforehand via terminal).
func (h *Handler) StartAgent(hc ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.PtyStartAgentRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "pty.start_agent: invalid payload: "+err.Error())
	}

	agentBinaries := map[string]string{
		"claude": "claude",
		"codex":  "codex",
		"cursor": "cursor",
	}
	bin, ok := agentBinaries[req.Agent]
	if !ok {
		return nil, errBody(ws.ErrCodePtyUnknownAgent, "pty.start_agent: unknown agent "+req.Agent)
	}

	if n := h.countByConn(hc.ConnID); n >= MaxPTYsPerConn {
		return nil, errBody(ws.ErrCodePtyLimitReached,
			fmt.Sprintf("pty.start_agent: connection already owns %d PTYs (cap %d)", n, MaxPTYsPerConn))
	}

	cwd := h.WorkspaceRoot
	if req.Cwd != nil && *req.Cwd != "" {
		resolved, err := h.resolveCwd(*req.Cwd)
		if err != nil {
			return nil, errBody(ws.ErrCodePtySpawnFailed, "pty.start_agent: "+err.Error())
		}
		cwd = resolved
	}

	args := make([]string, len(req.Args))
	copy(args, req.Args)
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = mergeEnv(map[string]string(req.Env))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Default initial size; FE is expected to send pty.resize shortly after
	// (same pattern as a fresh terminal tab).
	const defaultCols, defaultRows = 80, 24

	file, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(defaultRows),
		Cols: uint16(defaultCols),
	})
	if err != nil {
		return nil, errBody(ws.ErrCodePtySpawnFailed, "pty.start_agent: "+err.Error())
	}

	s := &session{
		id:     uuid.NewString(),
		connID: hc.ConnID,
		cmd:    cmd,
		file:   file,
		done:   make(chan struct{}),
	}
	h.register(s)

	publisher := hc.Publisher
	go h.outputLoop(s, publisher)
	go h.waitLoop(s, publisher)

	resp, err := json.Marshal(protogen.PtyStartAgentResponse{PtyID: s.id})
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "pty.start_agent: marshal: "+err.Error())
	}
	return resp, nil
}

// Input implements pty.input. Fire-and-forget — success is silent, failures
// come back via the error envelope correlated by request id.
func (h *Handler) Input(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.PtyInput
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "pty.input: invalid payload: "+err.Error())
	}
	s := h.lookup(req.PtyID)
	if s == nil {
		return nil, errBody(ws.ErrCodePtyNotFound, "pty.input: unknown pty_id")
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "pty.input: invalid base64: "+err.Error())
	}
	if _, err := s.file.Write(data); err != nil {
		return nil, errBody(ws.ErrCodePtyWriteFailed, "pty.input: "+err.Error())
	}
	// nil payload + nil error == fire-and-forget. Server writes no response.
	return nil, nil
}

// Resize implements pty.resize.
func (h *Handler) Resize(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.PtyResizeRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "pty.resize: invalid payload: "+err.Error())
	}
	if req.Cols < 1 || req.Cols > 1000 || req.Rows < 1 || req.Rows > 1000 {
		return nil, errBody(ws.ErrCodePtyInvalidSize, "pty.resize: cols/rows out of bounds")
	}
	s := h.lookup(req.PtyID)
	if s == nil {
		return nil, errBody(ws.ErrCodePtyNotFound, "pty.resize: unknown pty_id")
	}
	if err := pty.Setsize(s.file, &pty.Winsize{
		Rows: uint16(req.Rows),
		Cols: uint16(req.Cols),
	}); err != nil {
		return nil, errBody(ws.ErrCodePtyWriteFailed, "pty.resize: "+err.Error())
	}
	return json.RawMessage(`{}`), nil
}

// Close implements pty.close. Idempotent: closing an unknown / already-closed
// pty_id returns success.
func (h *Handler) Close(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.PtyCloseRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "pty.close: invalid payload: "+err.Error())
	}
	if s := h.lookup(req.PtyID); s != nil {
		h.teardown(s)
	}
	return json.RawMessage(`{}`), nil
}

// OnDisconnect is the ws.ConnLifecycle hook. SIGTERM all sessions owned by
// connID, wait closeGrace, SIGKILL stragglers. Runs synchronously from the
// per-conn cleanup so the cleanup goroutine is the one that's holding the
// shells alive long enough to actually receive the signals.
func (h *Handler) OnDisconnect(connID string) {
	owned := h.sessionsByConn(connID)
	if len(owned) == 0 {
		return
	}
	for _, s := range owned {
		_ = h.teardown(s)
	}
}

// outputLoop reads PTY master → publishes pty.output until EOF / PTY closed.
// On Publish drops, increments a counter and surfaces it via a
// pty.output_dropped event on the next successful publish.
func (h *Handler) outputLoop(s *session, pub ws.Publisher) {
	buf := make([]byte, readBufSize)
	droppedSinceFlush := 0
	for {
		n, err := s.file.Read(buf)
		if n > 0 {
			if droppedSinceFlush > 0 {
				dropEv, mErr := json.Marshal(protogen.PtyOutputDroppedEvent{
					PtyID:        s.id,
					DroppedCount: droppedSinceFlush,
				})
				if mErr == nil && pub.Publish("pty.output_dropped", dropEv) {
					droppedSinceFlush = 0
				}
				// If even the drop notification was dropped, keep the count
				// pending and try again next iteration. Better one truthful
				// late notification than a lie.
			}
			out := buf[:n]
			ev, mErr := json.Marshal(protogen.PtyOutputEvent{
				PtyID: s.id,
				Data:  base64.StdEncoding.EncodeToString(out),
			})
			if mErr == nil {
				if !pub.Publish("pty.output", ev) {
					droppedSinceFlush++
				}
			}
		}
		if err != nil {
			// EOF / closed master / read error — bail. The wait goroutine
			// will publish pty.exit; we just need to stop reading.
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				// pathSep err from a closed master typically arrives as a
				// PathError wrapping EBADF. We log nothing for v1 — the
				// per-frame log noise on every close isn't worth it.
				_ = err
			}
			return
		}
	}
}

// waitLoop awaits the child process and publishes the pty.exit event. Runs
// exactly once per session.
func (h *Handler) waitLoop(s *session, pub ws.Publisher) {
	defer close(s.done)

	waitErr := s.cmd.Wait()
	// Once the process has exited, closing the master fd unblocks the read
	// loop and frees the kernel-side PTY pair.
	_ = s.file.Close()

	exitCode, signalName := classifyExit(s.cmd.ProcessState, waitErr)

	h.unregister(s.id)

	ev := protogen.PtyExitEvent{
		PtyID:    s.id,
		ExitCode: exitCode,
	}
	if signalName != "" {
		sigCopy := signalName
		ev.Signal = &sigCopy
	}
	body, err := json.Marshal(ev)
	if err == nil {
		pub.Publish("pty.exit", body)
	}
}

// teardown signals the process to terminate. Idempotent — concurrent callers
// (pty.close + OnDisconnect during a tab refresh) race safely. Returns the
// session that was torn down for convenience.
func (h *Handler) teardown(s *session) *session {
	if !s.closing.CompareAndSwap(false, true) {
		return s
	}

	// Best-effort SIGTERM the whole process group (cmd ran with Setsid so
	// pid == pgid). The waitLoop will see Wait() return and clean up.
	if proc := s.cmd.Process; proc != nil {
		_ = syscall.Kill(-proc.Pid, syscall.SIGTERM)
	}

	// Grace window, then escalate to SIGKILL. We don't block the calling
	// goroutine on close — the cleanup runs in the background so a typing
	// user doesn't see "exiting…" hang the UI.
	go func() {
		select {
		case <-s.done:
			return
		case <-time.After(closeGrace):
		}
		if proc := s.cmd.Process; proc != nil {
			_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
		}
		// As a last resort make sure the master file is closed even if the
		// process is wedged — read loop will then exit on EBADF.
		_ = s.file.Close()
	}()
	return s
}

// --- registry helpers -------------------------------------------------------

func (h *Handler) register(s *session) {
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
}

func (h *Handler) unregister(id string) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}

func (h *Handler) lookup(id string) *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id]
}

func (h *Handler) countByConn(connID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.sessions {
		if s.connID == connID {
			n++
		}
	}
	return n
}

// OpenSessionCount returns the number of live PTY sessions across all
// connections. Exposed for tests and future health/observability hooks.
func (h *Handler) OpenSessionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

func (h *Handler) sessionsByConn(connID string) []*session {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*session
	for _, s := range h.sessions {
		if s.connID == connID {
			out = append(out, s)
		}
	}
	return out
}

// --- shell selection, env merging, cwd sandbox ------------------------------

// pickShell prefers $SHELL, then /bin/bash, then /bin/sh. No --login: avoids
// /etc/profile latency on first keystroke. ~/.bashrc still runs for
// interactive shells.
func pickShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

// mergeEnv layers the request's env vars on top of the daemon's process
// env. TERM is not defaulted here — the frontend sets it to xterm-256color
// at open time (decision §0.10).
func mergeEnv(extra map[string]string) []string {
	base := os.Environ()
	if len(extra) == 0 {
		return base
	}
	keys := make(map[string]bool, len(extra))
	for k := range extra {
		keys[k] = true
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			out = append(out, kv)
			continue
		}
		if keys[kv[:i]] {
			continue // overridden below
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

// resolveCwd path-sandboxes a request cwd against WorkspaceRoot. Same logic
// as internal/fs/handler.go::resolve — duplicated here to avoid an internal
// dependency cycle.
func (h *Handler) resolveCwd(reqPath string) (string, error) {
	if filepath.IsAbs(reqPath) {
		return "", errors.New("cwd: absolute paths are not allowed; use workspace-relative")
	}
	joined := filepath.Join(h.WorkspaceRoot, reqPath)
	clean := filepath.Clean(joined)
	rootClean := filepath.Clean(h.WorkspaceRoot)
	if clean == rootClean {
		return clean, nil
	}
	rel, err := filepath.Rel(rootClean, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("cwd: path escapes workspace root")
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("cwd: path is not a directory")
	}
	return clean, nil
}

// classifyExit collapses the (ProcessState, Wait-err) pair into the
// {exit_code, signal} the FE wants. exit_code is -1 on signal.
func classifyExit(ps *os.ProcessState, _ error) (int, string) {
	if ps == nil {
		return -1, ""
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return -1, signalName(ws.Signal())
	}
	return ps.ExitCode(), ""
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	default:
		return sig.String()
	}
}

func errBody(code, msg string) *protogen.EnvelopeError {
	return &protogen.EnvelopeError{Code: code, Message: msg}
}
