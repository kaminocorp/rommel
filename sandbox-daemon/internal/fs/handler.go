// Package fs implements the fs.* primitives. Today only fs.read is real;
// fs.write, fs.list, and fs.watch return not_implemented so the surface area
// is visible without pretending to work.
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

// NotImplemented is the stub returned for fs.write, fs.list, fs.watch.
func (h *Handler) NotImplemented(verb string) ws.HandlerFunc {
	return func(_ context.Context, _ *protogen.SessionTokenClaims, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
		return nil, errBody(ws.ErrCodeNotImplemented, verb+": not implemented in scaffolding phase")
	}
}

// resolve sandboxes a request path. Absolute paths are rejected outright;
// otherwise the path is joined to Root, cleaned, and re-verified to live
// under Root.
func (h *Handler) resolve(reqPath string) (string, error) {
	if reqPath == "" {
		return "", errors.New("fs.read: path is empty")
	}
	if filepath.IsAbs(reqPath) {
		return "", errors.New("fs.read: absolute paths are not allowed; use workspace-relative")
	}
	joined := filepath.Join(h.Root, reqPath)
	clean := filepath.Clean(joined)

	// Final check: clean(root + path) must still be under root.
	rootClean := filepath.Clean(h.Root)
	rel, err := filepath.Rel(rootClean, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("fs.read: path escapes workspace root")
	}
	return clean, nil
}

func errBody(code, msg string) *protogen.EnvelopeError {
	return &protogen.EnvelopeError{Code: code, Message: msg}
}
