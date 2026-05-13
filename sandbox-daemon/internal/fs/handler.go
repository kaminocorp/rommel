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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

type Handler struct {
	Root string // absolute workspace root
}

// Read implements fs.read.
func (h *Handler) Read(_ context.Context, _ *protogen.SessionTokenClaims, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
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
func (h *Handler) List(_ context.Context, _ *protogen.SessionTokenClaims, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
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
func (h *Handler) Write(_ context.Context, _ *protogen.SessionTokenClaims, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
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

// NotImplemented is the stub returned for primitives that don't have a real
// handler yet (fs.watch in v1).
func (h *Handler) NotImplemented(verb string) ws.HandlerFunc {
	return func(_ context.Context, _ *protogen.SessionTokenClaims, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
		return nil, errBody(ws.ErrCodeNotImplemented, verb+": not implemented")
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

func errBody(code, msg string) *protogen.EnvelopeError {
	return &protogen.EnvelopeError{Code: code, Message: msg}
}
