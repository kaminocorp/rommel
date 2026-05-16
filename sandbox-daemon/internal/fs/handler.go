// Package fs implements the fs.* primitives.
//
// Path sandbox: every request path is treated as workspace-relative, joined
// against Config.WorkspaceRoot, cleaned with filepath.Clean, and rejected if
// the result no longer has the workspace root as a prefix. This catches
// absolute paths and `..` escapes. Symlink-following is deliberately not
// resolved at this layer (see scaffolding plan §2 confirmation: prefix check
// only for v1; EvalSymlinks revisits when the daemon is real).
package fs

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"
	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

const maxWatchesPerConn = 32

// watchEntry tracks one active fs.watch subscription for a connection.
type watchEntry struct {
	path      string
	recursive bool
	pub       ws.Publisher
}

type Handler struct {
	Root string // absolute workspace root

	watcher *fsnotify.Watcher
	wmu     sync.Mutex // protects watches + watcher adds/removes + connDropped + watcherErr
	// connID -> (relative path -> entry)
	watches map[string]map[string]watchEntry

	// abs path -> refcount (how many active recursive or direct watches cover this path)
	watched map[string]int

	// connDropped tracks connections whose OnDisconnect has fired. Even though
	// OnDisconnect removes the connID's entries from `watches`, an in-flight
	// event already in the runEventLoop pipeline can still hold a Publisher
	// pointer captured at Watch() time. handleFsEvent consults this map to
	// avoid publishing into a torn-down connection's pump (which would race
	// with the pump's own shutdown). Defense in depth.
	connDropped map[string]bool

	// watcherErr captures a one-time fsnotify.NewWatcher failure so the next
	// fs.watch call can return a proper fs.watch_failed envelope instead of
	// panicking on a still-rare-but-real init failure (file-descriptor
	// exhaustion on a slammed sandbox, kernel inotify limit, etc).
	watcherErr error

	eventLoopOnce sync.Once
	stopCh        chan struct{}
	stopped       bool
}

// Read implements fs.read.
func (h *Handler) Read(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FsReadRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "fs.read: invalid payload: "+err.Error())
	}

	abs, err := h.resolve(req.Path)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, err.Error())
	}

	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, errBody(ws.ErrCodeFsNotFound, "fs.read: no such file: "+req.Path)
	case err != nil:
		return nil, errBody(ws.ErrCodeFsIO, "fs.read: stat: "+err.Error())
	case info.IsDir():
		return nil, errBody(ws.ErrCodeFsInvalidPath, "fs.read: path is a directory: "+req.Path)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsIO, "fs.read: read: "+err.Error())
	}

	enc := req.Encoding
	if enc == "" {
		enc = protogen.FsReadRequestEncodingUtf8
	}

	var contents string
	var respEnc protogen.FsReadResponseEncoding
	switch enc {
	case protogen.FsReadRequestEncodingUtf8:
		if !utf8.Valid(data) {
			return nil, errBody(ws.ErrCodeBadRequest, "fs.read: file is not valid utf-8; request encoding=base64")
		}
		contents = string(data)
		respEnc = protogen.FsReadResponseEncodingUtf8
	case protogen.FsReadRequestEncodingBase64:
		contents = base64.StdEncoding.EncodeToString(data)
		respEnc = protogen.FsReadResponseEncodingBase64
	default:
		return nil, errBody(ws.ErrCodeBadRequest, "fs.read: unsupported encoding: "+string(enc))
	}

	resp := protogen.FsReadResponse{
		Path:     req.Path,
		Contents: contents,
		Encoding: respEnc,
		Size:     int(info.Size()),
		Mtime:    info.ModTime().UTC(),
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "fs.read: marshal response: "+err.Error())
	}
	return out, nil
}

// List implements fs.list. One level deep, sorted by name. Hidden files
// included.
func (h *Handler) List(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FsListRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "fs.list: invalid payload: "+err.Error())
	}

	abs, err := h.resolve(req.Path)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, err.Error())
	}

	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, errBody(ws.ErrCodeFsNotFound, "fs.list: no such directory: "+req.Path)
	case err != nil:
		return nil, errBody(ws.ErrCodeFsIO, "fs.list: stat: "+err.Error())
	case !info.IsDir():
		return nil, errBody(ws.ErrCodeFsInvalidPath, "fs.list: path is not a directory: "+req.Path)
	}

	dirents, err := os.ReadDir(abs)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsIO, "fs.list: readdir: "+err.Error())
	}

	entries := make([]protogen.FsListEntry, 0, len(dirents))
	for _, d := range dirents {
		fi, ierr := d.Info()
		if ierr != nil {
			// Stat race (entry vanished mid-listing). Skip silently — caller
			// will re-list if it cares.
			continue
		}
		kind := protogen.FsListEntryKindFile
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			kind = protogen.FsListEntryKindSymlink
		case fi.IsDir():
			kind = protogen.FsListEntryKindDir
		}
		entries = append(entries, protogen.FsListEntry{
			Name:  fi.Name(),
			Kind:  kind,
			Size:  int(fi.Size()),
			Mtime: fi.ModTime().UTC(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	resp := protogen.FsListResponse{
		Path:    req.Path,
		Entries: entries,
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "fs.list: marshal response: "+err.Error())
	}
	return out, nil
}

// Write implements fs.write. Full-content overwrite. Parent directory must
// already exist (fs.mkdir is a separate primitive — not yet implemented).
func (h *Handler) Write(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FsWriteRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "fs.write: invalid payload: "+err.Error())
	}

	abs, err := h.resolve(req.Path)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, err.Error())
	}

	enc := req.Encoding
	if enc == "" {
		enc = protogen.FsWriteRequestEncodingUtf8
	}
	var data []byte
	switch enc {
	case protogen.FsWriteRequestEncodingUtf8:
		data = []byte(req.Contents)
	case protogen.FsWriteRequestEncodingBase64:
		decoded, derr := base64.StdEncoding.DecodeString(req.Contents)
		if derr != nil {
			return nil, errBody(ws.ErrCodeBadRequest, "fs.write: invalid base64: "+derr.Error())
		}
		data = decoded
	default:
		return nil, errBody(ws.ErrCodeBadRequest, "fs.write: unsupported encoding: "+string(enc))
	}

	// Refuse to overwrite a directory — surfaces fs.invalid_path, not fs.io.
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		return nil, errBody(ws.ErrCodeFsInvalidPath, "fs.write: path is a directory: "+req.Path)
	}

	if err := os.WriteFile(abs, data, 0o644); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Parent dir missing — surfaces fs.not_found so the FE can show
			// a useful message instead of a generic I/O error.
			return nil, errBody(ws.ErrCodeFsNotFound, "fs.write: parent directory does not exist (no fs.mkdir yet): "+filepath.Dir(req.Path))
		}
		return nil, errBody(ws.ErrCodeFsIO, "fs.write: "+err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsIO, "fs.write: post-stat: "+err.Error())
	}

	resp := protogen.FsWriteResponse{
		Path:  req.Path,
		Size:  int(info.Size()),
		Mtime: info.ModTime().UTC(),
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "fs.write: marshal response: "+err.Error())
	}
	return out, nil
}

// Mkdir implements fs.mkdir (Phase 4). Creates a directory.
// - recursive=false (default): parent must exist; fails with fs.not_found if parent missing.
// - recursive=true: creates parents as needed (mkdir -p).
// If the path already exists as a directory, returns success (idempotent).
// If it exists as a file, returns fs.exists.
func (h *Handler) Mkdir(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FsMkdirRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "fs.mkdir: invalid payload: "+err.Error())
	}

	abs, err := h.resolve(req.Path)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, err.Error())
	}

	// Check current state
	if info, statErr := os.Stat(abs); statErr == nil {
		if info.IsDir() {
			// Already exists as dir — success (idempotent)
			resp := protogen.FsMkdirResponse{
				Path:  req.Path,
				Mtime: info.ModTime().UTC(),
			}
			out, _ := json.Marshal(resp)
			return out, nil
		}
		return nil, errBody(ws.ErrCodeFsExists, "fs.mkdir: path exists and is not a directory: "+req.Path)
	}

	if req.Recursive {
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, errBody(ws.ErrCodeFsIO, "fs.mkdir: "+err.Error())
		}
	} else {
		if err := os.Mkdir(abs, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return nil, errBody(ws.ErrCodeFsExists, "fs.mkdir: path already exists: "+req.Path)
			}
			if errors.Is(err, fs.ErrNotExist) {
				return nil, errBody(ws.ErrCodeFsNotFound, "fs.mkdir: parent directory does not exist: "+filepath.Dir(req.Path))
			}
			return nil, errBody(ws.ErrCodeFsIO, "fs.mkdir: "+err.Error())
		}
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsIO, "fs.mkdir: post-stat: "+err.Error())
	}

	resp := protogen.FsMkdirResponse{
		Path:  req.Path,
		Mtime: info.ModTime().UTC(),
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "fs.mkdir: marshal: "+err.Error())
	}
	return out, nil
}

// Move implements fs.move (Phase 4) — atomic rename within the workspace.
// Source must exist. Destination parent must exist. Destination must not exist.
func (h *Handler) Move(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FsMoveRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "fs.move: invalid payload: "+err.Error())
	}

	fromAbs, err := h.resolve(req.From)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, "fs.move from: "+err.Error())
	}
	toAbs, err := h.resolve(req.To)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, "fs.move to: "+err.Error())
	}

	// Source must exist
	if _, err := os.Stat(fromAbs); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errBody(ws.ErrCodeFsNotFound, "fs.move: source does not exist: "+req.From)
		}
		return nil, errBody(ws.ErrCodeFsIO, "fs.move: stat source: "+err.Error())
	}

	// Destination must not exist
	if _, err := os.Stat(toAbs); err == nil {
		return nil, errBody(ws.ErrCodeFsExists, "fs.move: destination already exists: "+req.To)
	}

	// Destination parent must exist
	toDir := filepath.Dir(toAbs)
	if toDir != h.Root { // allow moving into root
		if info, err := os.Stat(toDir); err != nil || !info.IsDir() {
			return nil, errBody(ws.ErrCodeFsNotFound, "fs.move: destination parent does not exist: "+filepath.Dir(req.To))
		}
	}

	if err := os.Rename(fromAbs, toAbs); err != nil {
		return nil, errBody(ws.ErrCodeFsIO, "fs.move: rename: "+err.Error())
	}

	info, err := os.Stat(toAbs)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsIO, "fs.move: post-stat: "+err.Error())
	}

	resp := protogen.FsMoveResponse{
		From:  req.From,
		To:    req.To,
		Mtime: info.ModTime().UTC(),
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "fs.move: marshal: "+err.Error())
	}
	return out, nil
}

// Delete implements fs.delete (Phase 4).
// - For files: always removes.
// - For directories: if recursive=false and dir is non-empty → fs.not_empty.
// - If path does not exist → fs.not_found (non-idempotent by design so callers can react).
func (h *Handler) Delete(_ ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FsDeleteRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "fs.delete: invalid payload: "+err.Error())
	}

	abs, err := h.resolve(req.Path)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, err.Error())
	}

	info, statErr := os.Stat(abs)
	if statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil, errBody(ws.ErrCodeFsNotFound, "fs.delete: path does not exist: "+req.Path)
		}
		return nil, errBody(ws.ErrCodeFsIO, "fs.delete: stat: "+statErr.Error())
	}

	if info.IsDir() && !req.Recursive {
		// Check if empty
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, errBody(ws.ErrCodeFsIO, "fs.delete: readdir: "+err.Error())
		}
		if len(entries) > 0 {
			return nil, errBody(ws.ErrCodeFsNotEmpty, "fs.delete: directory is not empty (use recursive=true): "+req.Path)
		}
	}

	if info.IsDir() {
		if err := os.RemoveAll(abs); err != nil {
			return nil, errBody(ws.ErrCodeFsIO, "fs.delete: removeall: "+err.Error())
		}
	} else {
		if err := os.Remove(abs); err != nil {
			return nil, errBody(ws.ErrCodeFsIO, "fs.delete: remove: "+err.Error())
		}
	}

	resp := protogen.FsDeleteResponse{Path: req.Path}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "fs.delete: marshal: "+err.Error())
	}
	return out, nil
}

// NotImplemented is the stub returned for primitives that don't have a real
// handler yet.
func (h *Handler) NotImplemented(verb string) ws.HandlerFunc {
	return func(_ ws.HandlerCtx, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
		return nil, errBody(ws.ErrCodeNotImplemented, verb+": not implemented")
	}
}

// Watch implements fs.watch (Phase 1). It starts a filesystem watch for the
// calling connection. Real OS-level watching (fsnotify + recursive directory
// tracking + change coalescing) is implemented in the full version; this
// skeleton proves the five-seam (schema, dispatch, handler, Publisher, FE subscribe)
// and the ConnLifecycle cleanup contract.
func (h *Handler) Watch(hc ws.HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FsWatchRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "fs.watch: invalid payload: "+err.Error())
	}

	abs, err := h.resolve(req.Path)
	if err != nil {
		return nil, errBody(ws.ErrCodeFsInvalidPath, err.Error())
	}

	h.wmu.Lock()
	defer h.wmu.Unlock()

	if h.watches == nil {
		h.watches = make(map[string]map[string]watchEntry)
	}
	connWatches := h.watches[hc.ConnID]
	if connWatches == nil {
		connWatches = make(map[string]watchEntry)
		h.watches[hc.ConnID] = connWatches
	}
	if len(connWatches) >= maxWatchesPerConn {
		return nil, errBody(ws.ErrCodeFsWatchLimitReached, "fs.watch: too many watches on this connection")
	}

	// Start the real watcher + event loop on first use. Init failures
	// (file-descriptor exhaustion, inotify limit) surface here as
	// fs.watch_failed — the daemon used to panic at this point.
	if err := h.ensureWatcherLocked(); err != nil {
		return nil, errBody(ws.ErrCodeFsWatchFailed, "fs.watch: "+err.Error())
	}

	// Register the subscription
	connWatches[req.Path] = watchEntry{
		path:      req.Path,
		recursive: req.Recursive,
		pub:       hc.Publisher,
	}

	// Actually add to fsnotify
	if req.Recursive {
		h.addRecursiveLocked(abs)
	} else {
		h.addPathLocked(abs)
	}

	resp := protogen.FsWatchResponse{Path: req.Path}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, "fs.watch: marshal: "+err.Error())
	}
	return out, nil
}

// OnDisconnect implements ws.ConnLifecycle. Called by the WS server when a
// connection drops (including normal close or network error). We drop every
// watch owned by that connID and stop watching the underlying paths if they
// have no remaining subscribers.
func (h *Handler) OnDisconnect(connID string) {
	h.wmu.Lock()
	defer h.wmu.Unlock()
	if h.connDropped == nil {
		h.connDropped = make(map[string]bool)
	}
	// Mark first; any handleFsEvent that's already past its wmu acquire is
	// still publishing to entries it captured before this — that's fine,
	// they're being torn down anyway. Future event passes will see the flag.
	h.connDropped[connID] = true
	if h.watches == nil {
		return
	}
	paths, ok := h.watches[connID]
	if !ok {
		return
	}
	for relPath := range paths {
		abs := filepath.Join(h.Root, relPath)
		h.removePathLocked(abs)
	}
	delete(h.watches, connID)
}

// OpenWatchCount returns the total number of live fs.watch subscriptions
// across all connections. Exposed for tests and future health/observability
// hooks (mirrors pty.Handler.OpenSessionCount).
func (h *Handler) OpenWatchCount() int {
	h.wmu.Lock()
	defer h.wmu.Unlock()
	n := 0
	for _, paths := range h.watches {
		n += len(paths)
	}
	return n
}

// Stop releases the underlying fsnotify watcher and stops the event loop.
// Idempotent; safe to call from cmd/daemon's signal handler regardless of
// whether fs.watch was ever invoked. After Stop, fs.watch calls will be
// rejected with fs.watch_failed (the daemon is shutting down anyway).
func (h *Handler) Stop() {
	h.wmu.Lock()
	if h.stopped {
		h.wmu.Unlock()
		return
	}
	h.stopped = true
	w := h.watcher
	stopCh := h.stopCh
	h.watcher = nil
	h.watches = nil
	h.watched = nil
	h.wmu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	if w != nil {
		_ = w.Close()
	}
}

// resolve sandboxes a request path. Absolute paths are rejected outright;
// otherwise the path is joined to Root, cleaned, and re-verified to live
// under Root.
func (h *Handler) resolve(reqPath string) (string, error) {
	if reqPath == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(reqPath) {
		return "", errors.New("absolute paths are not allowed; use workspace-relative")
	}
	joined := filepath.Join(h.Root, reqPath)
	clean := filepath.Clean(joined)

	// Final check: clean(root + path) must still be under root.
	rootClean := filepath.Clean(h.Root)
	if clean == rootClean {
		return clean, nil
	}
	rel, err := filepath.Rel(rootClean, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes workspace root")
	}
	return clean, nil
}

// --- Real fs.watch implementation (Phase 5) ---------------------------------

func (h *Handler) ensureWatcherLocked() error {
	if h.stopped {
		return errors.New("fs.watch: handler is stopped")
	}
	if h.watcher != nil {
		return nil
	}
	if h.watcherErr != nil {
		// We tried before and failed. Don't keep retrying — fsnotify init
		// failures are systemic (fd / inotify limits), not transient.
		return h.watcherErr
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		h.watcherErr = err
		return err
	}
	h.watcher = w
	h.watched = make(map[string]int)
	h.stopCh = make(chan struct{})

	go h.runEventLoop()
	return nil
}

func (h *Handler) addPathLocked(abs string) {
	if h.watcher == nil {
		return
	}
	if h.watched[abs] == 0 {
		_ = h.watcher.Add(abs)
	}
	h.watched[abs]++
}

func (h *Handler) addRecursiveLocked(abs string) {
	// Walk the tree and add every directory
	_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			h.addPathLocked(path)
		}
		return nil
	})
}

func (h *Handler) removePathLocked(abs string) {
	if h.watcher == nil || h.watched == nil {
		return
	}
	if h.watched[abs] <= 1 {
		_ = h.watcher.Remove(abs)
		delete(h.watched, abs)
	} else {
		h.watched[abs]--
	}
}

func (h *Handler) runEventLoop() {
	// Capture watcher + stopCh once. Stop() races with this goroutine: it
	// nils out h.watcher to disarm new Watch() calls, but the loop below
	// still needs a stable pointer to drain channels until they close.
	h.wmu.Lock()
	w := h.watcher
	stopCh := h.stopCh
	h.wmu.Unlock()
	if w == nil || stopCh == nil {
		return
	}
	for {
		select {
		case <-stopCh:
			return
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			h.handleFsEvent(event)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			// Log to stderr for operator visibility (no Publisher here)
			fmt.Fprintf(os.Stderr, "fs.watch error: %v\n", err)
		}
	}
}

func (h *Handler) handleFsEvent(event fsnotify.Event) {
	h.wmu.Lock()
	defer h.wmu.Unlock()

	if h.watches == nil {
		return
	}

	// Determine event type
	var eventType string
	switch {
	case event.Op.Has(fsnotify.Create):
		eventType = "created"
	case event.Op.Has(fsnotify.Write):
		eventType = "modified"
	case event.Op.Has(fsnotify.Remove):
		eventType = "deleted"
	case event.Op.Has(fsnotify.Rename):
		eventType = "moved"
	default:
		return // Chmod or other — ignore in v1
	}

	// If a directory was created inside a recursive watch, auto-add it
	if event.Op.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			// Check if any recursive watch covers this new dir
			for _, connWatches := range h.watches {
				for _, entry := range connWatches {
					if entry.recursive {
						absWatch := filepath.Join(h.Root, entry.path)
						if strings.HasPrefix(event.Name, absWatch+string(filepath.Separator)) || event.Name == absWatch {
							h.addRecursiveLocked(event.Name)
							break
						}
					}
				}
			}
		}
	}

	// Find all active watches that should receive this event. Skip any
	// connection whose OnDisconnect has fired since the event was queued —
	// the pump on the far side is already (or about to be) closed, and the
	// owning runConn goroutine is winding down.
	for connID, connWatches := range h.watches {
		if h.connDropped[connID] {
			continue
		}
		for _, entry := range connWatches {
			absWatch := filepath.Join(h.Root, entry.path)
			rel, err := filepath.Rel(absWatch, event.Name)
			if err != nil {
				continue
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if !entry.recursive && rel != "." && !strings.HasPrefix(rel, "."+string(filepath.Separator)) {
				// Non-recursive watch only cares about the exact path
				if event.Name != absWatch {
					continue
				}
			}

			// Build and publish the event
			ev := protogen.FsWatchEvent{
				Path: event.Name, // absolute on wire? Better to make it relative to workspace root for consistency with other fs.*
				Type: eventType,
			}
			// Make path relative for the client (matches fs.list/read semantics)
			if relPath, err := filepath.Rel(h.Root, event.Name); err == nil {
				ev.Path = relPath
			}

			// For moved events we try to provide old_path when possible (best effort)
			if eventType == "moved" {
				// fsnotify doesn't reliably give old name in the same event on all platforms.
				// For v1 we leave OldPath empty; consumers can treat "moved" + path as "something moved here".
			}

			payload, _ := json.Marshal(ev)
			entry.pub.Publish("fs.watch-event", payload)
		}
	}
}


func errBody(code, msg string) *protogen.EnvelopeError {
	return &protogen.EnvelopeError{Code: code, Message: msg}
}
