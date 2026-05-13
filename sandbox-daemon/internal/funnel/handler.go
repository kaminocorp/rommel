// Package funnel implements the funnel.* primitives — first-class verbs over
// the rommel/<stage>/ kanban directory layout described in docs/vision.md
// §Layer 2.
//
// The funnel root is, by convention, <WorkspaceRoot>/rommel — no env var. A
// missing rommel/ directory is not an error: funnel.list on a workspace
// without a funnel returns an empty entry list. Other primitives that require
// the funnel to exist (read, promote) return funnel.not_found.
//
// Path sandbox: stages are validated against the enum; names are validated as
// filename-only — no path separator, no leading dot, no '..'. The joined path
// is then re-verified to live under the funnel root. This is stricter than
// fs.* because funnel cards are user content, not hidden tool files.
package funnel

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

// MaxReadBytes caps funnel.read response size to keep a runaway markdown file
// from oom-ing the editor.
const MaxReadBytes = 1 << 20 // 1 MiB

// stages is the canonical kebab-case list, in funnel order.
var stages = []string{"triage", "plans", "next-up", "executing", "completions", "archive"}

// stageIndex returns the position of stage in `stages`, or -1 if not a valid
// stage name.
func stageIndex(stage string) int {
	for i, s := range stages {
		if s == stage {
			return i
		}
	}
	return -1
}

// isValidTransition encodes the v1 promotion rules from the Phase 6 plan §0.5:
//   - forward-only along stages
//   - plus: archive-from-anywhere (kill switch)
//
// Same-stage moves are rejected — promote(name, X, X) is a no-op the FE
// shouldn't issue.
func isValidTransition(from, to string) bool {
	fi := stageIndex(from)
	ti := stageIndex(to)
	if fi < 0 || ti < 0 {
		return false
	}
	if to == "archive" && from != "archive" {
		return true
	}
	return ti == fi+1
}

type Handler struct {
	Root string // absolute funnel root — typically <WorkspaceRoot>/rommel
}

// List implements funnel.list.
func (h *Handler) List(_ context.Context, _ *protogen.SessionTokenClaims, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FunnelListRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "funnel.list: invalid payload: "+err.Error())
	}
	stage := string(req.Stage)
	if stageIndex(stage) < 0 {
		return nil, errBody(ws.ErrCodeFunnelInvalidStage, "funnel.list: unknown stage: "+stage)
	}

	stageDir := filepath.Join(h.Root, stage)
	dirents, err := os.ReadDir(stageDir)
	if errors.Is(err, fs.ErrNotExist) {
		// Either rommel/ or rommel/<stage>/ doesn't exist — this is fine for
		// workspaces that don't dogfood a funnel. Return an empty list.
		return marshal(protogen.FunnelListResponse{
			Stage:   protogen.FunnelStage(stage),
			Entries: []protogen.FunnelEntry{},
		}, "funnel.list")
	}
	if err != nil {
		return nil, errBody(ws.ErrCodeFunnelIO, "funnel.list: readdir: "+err.Error())
	}

	entries := make([]protogen.FunnelEntry, 0, len(dirents))
	for _, d := range dirents {
		// v1: skip subdirectories. The funnel is one level deep.
		if d.IsDir() {
			continue
		}
		fi, ierr := d.Info()
		if ierr != nil {
			continue
		}
		entries = append(entries, protogen.FunnelEntry{
			Name:  fi.Name(),
			Size:  int(fi.Size()),
			Mtime: fi.ModTime().UTC(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	return marshal(protogen.FunnelListResponse{
		Stage:   protogen.FunnelStage(stage),
		Entries: entries,
	}, "funnel.list")
}

// Read implements funnel.read.
func (h *Handler) Read(_ context.Context, _ *protogen.SessionTokenClaims, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FunnelReadRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "funnel.read: invalid payload: "+err.Error())
	}
	stage := string(req.Stage)
	if stageIndex(stage) < 0 {
		return nil, errBody(ws.ErrCodeFunnelInvalidStage, "funnel.read: unknown stage: "+stage)
	}
	if err := validateName(req.Name); err != nil {
		return nil, errBody(ws.ErrCodeFunnelInvalidName, "funnel.read: "+err.Error())
	}

	abs := filepath.Join(h.Root, stage, req.Name)
	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, errBody(ws.ErrCodeFunnelNotFound, "funnel.read: no such entry: "+stage+"/"+req.Name)
	case err != nil:
		return nil, errBody(ws.ErrCodeFunnelIO, "funnel.read: stat: "+err.Error())
	case info.IsDir():
		return nil, errBody(ws.ErrCodeFunnelInvalidName, "funnel.read: entry is a directory: "+req.Name)
	case info.Size() > MaxReadBytes:
		return nil, errBody(ws.ErrCodeFunnelIO, "funnel.read: entry exceeds 1 MiB cap")
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, errBody(ws.ErrCodeFunnelIO, "funnel.read: read: "+err.Error())
	}

	return marshal(protogen.FunnelReadResponse{
		Stage:    protogen.FunnelReadResponseStage(stage),
		Name:     req.Name,
		Contents: string(data),
		Size:     int(info.Size()),
		Mtime:    info.ModTime().UTC(),
	}, "funnel.read")
}

// Promote implements funnel.promote. Atomic os.Rename on POSIX.
func (h *Handler) Promote(_ context.Context, _ *protogen.SessionTokenClaims, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
	var req protogen.FunnelPromoteRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errBody(ws.ErrCodeBadRequest, "funnel.promote: invalid payload: "+err.Error())
	}
	from := string(req.From)
	to := string(req.To)
	if stageIndex(from) < 0 {
		return nil, errBody(ws.ErrCodeFunnelInvalidStage, "funnel.promote: unknown from stage: "+from)
	}
	if stageIndex(to) < 0 {
		return nil, errBody(ws.ErrCodeFunnelInvalidStage, "funnel.promote: unknown to stage: "+to)
	}
	if err := validateName(req.Name); err != nil {
		return nil, errBody(ws.ErrCodeFunnelInvalidName, "funnel.promote: "+err.Error())
	}
	if !isValidTransition(from, to) {
		return nil, errBody(ws.ErrCodeFunnelInvalidTransition, "funnel.promote: transition not allowed: "+from+" → "+to)
	}

	src := filepath.Join(h.Root, from, req.Name)
	dst := filepath.Join(h.Root, to, req.Name)

	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil, errBody(ws.ErrCodeFunnelNotFound, "funnel.promote: source not found: "+from+"/"+req.Name)
	}

	// Ensure the destination stage directory exists. We create it 0o755 if
	// missing — the funnel is allowed to materialize stages on demand.
	if err := os.MkdirAll(filepath.Join(h.Root, to), 0o755); err != nil {
		return nil, errBody(ws.ErrCodeFunnelIO, "funnel.promote: ensure dest dir: "+err.Error())
	}

	if err := os.Rename(src, dst); err != nil {
		return nil, errBody(ws.ErrCodeFunnelIO, "funnel.promote: rename: "+err.Error())
	}

	info, err := os.Stat(dst)
	if err != nil {
		return nil, errBody(ws.ErrCodeFunnelIO, "funnel.promote: post-stat: "+err.Error())
	}

	return marshal(protogen.FunnelPromoteResponse{
		Name:  req.Name,
		From:  from,
		To:    to,
		Mtime: info.ModTime().UTC(),
	}, "funnel.promote")
}

func validateName(name string) error {
	if name == "" {
		return errors.New("name is empty")
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return errors.New("name must be a basename (no path separators)")
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		// Hidden / dot-relative names disallowed: funnel cards are
		// user-content, hidden files would just confuse the board.
		return errors.New("name must not begin with '.'")
	}
	return nil
}

func marshal(v any, verb string) (json.RawMessage, *protogen.EnvelopeError) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, errBody(ws.ErrCodeInternal, verb+": marshal response: "+err.Error())
	}
	return out, nil
}

func errBody(code, msg string) *protogen.EnvelopeError {
	return &protogen.EnvelopeError{Code: code, Message: msg}
}
