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
)

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
