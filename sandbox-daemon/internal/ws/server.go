package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rommel-ade/rommel/sandbox-daemon/internal/auth"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/config"
	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
)

// HandlerFunc is the contract for one primitive. It receives the request
// payload as raw JSON (handlers unmarshal into their own typed shape) and
// returns either a response payload (marshalled to JSON) or an error envelope.
//
// Returning (nil, nil) is treated as a fire-and-forget — currently unused, but
// reserved for primitives that ack-via-event (e.g. pty.input).
type HandlerFunc func(ctx context.Context, claims *protogen.SessionTokenClaims, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError)

// Route binds a primitive name to its handler plus its capability requirement.
// RequiredScope is any-of: if non-empty, the token must carry at least one
// listed scope.
type Route struct {
	RequiredScope []protogen.SessionTokenClaimsScopeElem
	Fn            HandlerFunc
}

type Server struct {
	cfg    *config.Config
	routes map[string]Route
	up     websocket.Upgrader
}

func NewServer(cfg *config.Config, routes map[string]Route) *Server {
	return &Server{
		cfg:    cfg,
		routes: routes,
		up: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// v1 scaffolding: same-origin not required; the browser hits a
			// per-workspace URL behind Fly's internal DNS, not a public domain.
			// Tighten once the frontend's origin is known.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// HandleWS upgrades the connection at /ws?token=<jwt> and runs the per-conn loop.
func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	claims, err := auth.Verify(tok, s.cfg.TokenPublic, s.cfg.WID)
	if err != nil {
		log.Printf("ws: token rejected: %v", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote a response header; nothing to do.
		log.Printf("ws: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	s.runConn(r.Context(), conn, claims)
}

// HandleHealth answers GET /healthz with a static 200 — unauthenticated, used
// by Fly Machine probes and local smoke tests.
func (s *Server) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) runConn(ctx context.Context, conn *websocket.Conn, claims *protogen.SessionTokenClaims) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if !errors.Is(err, websocket.ErrCloseSent) {
				if ce, ok := err.(*websocket.CloseError); !ok || (ce.Code != websocket.CloseNormalClosure && ce.Code != websocket.CloseGoingAway) {
					log.Printf("ws: read error: %v", err)
				}
			}
			return
		}

		var frame Frame
		if err := json.Unmarshal(data, &frame); err != nil {
			s.writeFrame(conn, errorFrame(&Frame{}, ErrCodeBadRequest, "invalid envelope JSON: "+err.Error()))
			continue
		}
		if frame.Type == "" {
			s.writeFrame(conn, errorFrame(&frame, ErrCodeBadRequest, "envelope.type is required"))
			continue
		}
		if frame.Kind != protogen.EnvelopeKindRequest {
			s.writeFrame(conn, errorFrame(&frame, ErrCodeBadRequest, "only kind=request is accepted from the client today"))
			continue
		}

		out := s.dispatch(ctx, claims, &frame)
		if out == nil {
			continue // fire-and-forget
		}
		s.writeFrame(conn, out)
	}
}

func (s *Server) dispatch(ctx context.Context, claims *protogen.SessionTokenClaims, req *Frame) *Frame {
	route, ok := s.routes[req.Type]
	if !ok {
		return errorFrame(req, ErrCodeUnknownType, "unknown primitive: "+req.Type)
	}
	if !auth.HasAnyScope(claims, route.RequiredScope...) {
		return errorFrame(req, ErrCodeForbidden, "token lacks required scope for "+req.Type)
	}

	payload, errBody := route.Fn(ctx, claims, req.Payload)
	if errBody != nil {
		return &Frame{
			Kind:  protogen.EnvelopeKindError,
			Type:  req.Type,
			ID:    req.ID,
			Error: errBody,
		}
	}
	if payload == nil {
		return nil
	}
	return response(req, payload)
}

func (s *Server) writeFrame(conn *websocket.Conn, f *Frame) {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(f); err != nil {
		log.Printf("ws: write error: %v", err)
	}
}
