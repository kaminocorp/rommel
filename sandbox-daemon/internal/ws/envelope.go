// Package ws hosts the WebSocket transport and the per-message dispatch loop.
//
// On the wire, every message is an `Envelope` (proto/schemas/envelope.json).
// The generated `protogen.Envelope` uses `interface{}` for `payload`, which is
// awkward for handlers that want typed access to their request body. We use a
// local `Frame` type with `json.RawMessage` payload for I/O — same wire shape,
// nicer to dispatch through — and convert to/from the generated types at the
// boundary.
package ws

import (
	"encoding/json"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
)

// Frame is the wire-shape of an envelope, with `payload` left as raw JSON so
// the dispatcher can pass it through unchanged to a typed handler.
type Frame struct {
	Kind    protogen.EnvelopeKind   `json:"kind"`
	Type    string                  `json:"type"`
	ID      *string                 `json:"id,omitempty"`
	Payload json.RawMessage         `json:"payload,omitempty"`
	Error   *protogen.EnvelopeError `json:"error,omitempty"`
}

// Stable error codes. These travel on the wire and clients switch on them,
// so don't rename without coordinating the frontend (when it exists).
const (
	ErrCodeBadRequest     = "bad_request"
	ErrCodeNotImplemented = "not_implemented"
	ErrCodeUnknownType    = "unknown_type"
	ErrCodeForbidden      = "forbidden"
	ErrCodeInternal       = "internal"
	ErrCodeFsNotFound     = "fs.not_found"
	ErrCodeFsInvalidPath  = "fs.invalid_path"
	ErrCodeFsIO           = "fs.io"
	ErrCodeFsExists       = "fs.exists"
	ErrCodeFsNotEmpty     = "fs.not_empty"
	ErrCodeFsPermission   = "fs.permission"
	ErrCodeFsWatchFailed      = "fs.watch_failed"
	ErrCodeFsWatchLimitReached = "fs.watch_limit_reached"

	ErrCodeFunnelInvalidStage      = "funnel.invalid_stage"
	ErrCodeFunnelInvalidName       = "funnel.invalid_name"
	ErrCodeFunnelInvalidTransition = "funnel.invalid_transition"
	ErrCodeFunnelNotFound          = "funnel.not_found"
	ErrCodeFunnelIO                = "funnel.io"

	ErrCodePtyNotFound     = "pty.not_found"
	ErrCodePtySpawnFailed  = "pty.spawn_failed"
	ErrCodePtyWriteFailed  = "pty.write_failed"
	ErrCodePtyInvalidSize  = "pty.invalid_size"
	ErrCodePtyLimitReached = "pty.limit_reached"
	ErrCodePtyUnknownAgent = "pty.unknown_agent"
)

// eventKind is the envelope.kind for server-pushed events. Promoted out of a
// literal so the pump and the publishers stay in agreement.
const eventKind = protogen.EnvelopeKindEvent

// response builds a success envelope echoing the request id and type.
func response(req *Frame, payload json.RawMessage) *Frame {
	return &Frame{
		Kind:    protogen.EnvelopeKindResponse,
		Type:    req.Type,
		ID:      req.ID,
		Payload: payload,
	}
}

// errorFrame builds an error envelope echoing the request id and type.
func errorFrame(req *Frame, code, message string) *Frame {
	return &Frame{
		Kind: protogen.EnvelopeKindError,
		Type: req.Type,
		ID:   req.ID,
		Error: &protogen.EnvelopeError{
			Code:    code,
			Message: message,
		},
	}
}
