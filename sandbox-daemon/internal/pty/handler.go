// Package pty will host PTY primitives (pty.open, pty.input, pty.resize,
// pty.close, plus pty.output as a server-pushed event). For the scaffolding
// phase every verb returns not_implemented — the surface area is visible
// without pretending the wiring is real.
//
// When this becomes real, the implementation will use github.com/creack/pty
// to allocate the PTY pair and a per-pty_id goroutine to fan output back
// over the same WebSocket as `kind: event` envelopes.
package pty

import (
	"context"
	"encoding/json"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

type Handler struct{}

// NotImplemented returns the same not_implemented error envelope for every
// pty.* verb in the scaffolding phase.
func (h *Handler) NotImplemented(verb string) ws.HandlerFunc {
	return func(_ context.Context, _ *protogen.SessionTokenClaims, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
		return nil, &protogen.EnvelopeError{
			Code:    ws.ErrCodeNotImplemented,
			Message: verb + ": not implemented in scaffolding phase",
		}
	}
}
