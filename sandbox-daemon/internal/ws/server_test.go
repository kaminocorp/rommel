package ws_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
	"github.com/rommel-ade/rommel/sandbox-daemon/internal/config"
	fsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/fs"
	funnelx "github.com/rommel-ade/rommel/sandbox-daemon/internal/funnel"
	ptyx "github.com/rommel-ade/rommel/sandbox-daemon/internal/pty"
	wsx "github.com/rommel-ade/rommel/sandbox-daemon/internal/ws"
)

// --- test harness -----------------------------------------------------------

type harness struct {
	srv  *httptest.Server
	priv ed25519.PrivateKey
	cfg  *config.Config
	root string
	pty  *ptyx.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	root := t.TempDir()

	cfg := &config.Config{
		Port:          0, // not used by httptest
		WorkspaceRoot: root,
		WID:           "ws-test",
		TokenPublic:   pub,
	}

	fsh := &fsx.Handler{Root: cfg.WorkspaceRoot}
	funh := &funnelx.Handler{Root: filepath.Join(cfg.WorkspaceRoot, "rommel")}
	ptyh := ptyx.New(cfg.WorkspaceRoot)
	fsR := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemFsR,
		protogen.SessionTokenClaimsScopeElemFsRw,
	}
	fsRw := []protogen.SessionTokenClaimsScopeElem{protogen.SessionTokenClaimsScopeElemFsRw}
	ptyRw := []protogen.SessionTokenClaimsScopeElem{protogen.SessionTokenClaimsScopeElemPtyRw}
	funnelR := []protogen.SessionTokenClaimsScopeElem{
		protogen.SessionTokenClaimsScopeElemFunnelR,
		protogen.SessionTokenClaimsScopeElemFunnelRw,
	}
	funnelRw := []protogen.SessionTokenClaimsScopeElem{protogen.SessionTokenClaimsScopeElemFunnelRw}

	routes := map[string]wsx.Route{
		"system.ping": {Fn: func(_ wsx.HandlerCtx, _ json.RawMessage) (json.RawMessage, *protogen.EnvelopeError) {
			return json.RawMessage(`{"ok":true}`), nil
		}},
		"fs.read":        {RequiredScope: fsR, Fn: fsh.Read},
		"fs.list":        {RequiredScope: fsR, Fn: fsh.List},
		"fs.write":       {RequiredScope: fsRw, Fn: fsh.Write},
		"funnel.list":    {RequiredScope: funnelR, Fn: funh.List},
		"funnel.read":    {RequiredScope: funnelR, Fn: funh.Read},
		"funnel.promote": {RequiredScope: funnelRw, Fn: funh.Promote},
		"pty.open":       {RequiredScope: ptyRw, Fn: ptyh.Open},
		"pty.input":      {RequiredScope: ptyRw, Fn: ptyh.Input},
		"pty.resize":     {RequiredScope: ptyRw, Fn: ptyh.Resize},
		"pty.close":      {RequiredScope: ptyRw, Fn: ptyh.Close},
	}

	srv := wsx.NewServer(cfg, routes).WithLifecycle(ptyh)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.HandleHealth)
	mux.HandleFunc("/ws", srv.HandleWS)
	ts := httptest.NewServer(mux)

	t.Cleanup(func() { ts.Close() })

	return &harness{srv: ts, priv: priv, cfg: cfg, root: root, pty: ptyh}
}

// mintToken signs a valid JWT for this harness. Optional knobs let individual
// tests tweak just the field they care about (wid, scope, exp) without
// duplicating the claim bag.
type tokenOpts struct {
	wid    string
	scope  []string
	exp    time.Duration
	issuer string
	aud    string
}

func (h *harness) mintToken(t *testing.T, opts tokenOpts) string {
	t.Helper()
	if opts.wid == "" {
		opts.wid = h.cfg.WID
	}
	if opts.scope == nil {
		opts.scope = []string{"fs:rw", "pty:rw", "funnel:rw"}
	}
	if opts.exp == 0 {
		opts.exp = 5 * time.Minute
	}
	if opts.issuer == "" {
		opts.issuer = "rommel-backend"
	}
	if opts.aud == "" {
		opts.aud = "rommel-daemon"
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   opts.issuer,
		"sub":   "user-test",
		"aud":   opts.aud,
		"wid":   opts.wid,
		"scope": opts.scope,
		"exp":   now.Add(opts.exp).Unix(),
		"iat":   now.Unix(),
		"jti":   uuid.NewString(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	s, err := tok.SignedString(h.priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func (h *harness) dial(t *testing.T, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u, _ := url.Parse(h.srv.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	q := u.Query()
	if token != "" {
		q.Set("token", token)
	}
	u.RawQuery = q.Encode()
	return websocket.DefaultDialer.Dial(u.String(), nil)
}

func (h *harness) roundTrip(t *testing.T, conn *websocket.Conn, kind, typ string, payload any) *wsx.Frame {
	t.Helper()
	id := uuid.NewString()
	raw, _ := json.Marshal(payload)
	frame := wsx.Frame{
		Kind:    protogen.EnvelopeKind(kind),
		Type:    typ,
		ID:      &id,
		Payload: raw,
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Skip any pending server-pushed events (kind=event has no id). Phase 7
	// PTY introduces async pty.output / pty.exit events that can race ahead
	// of the next response on the wire.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var out wsx.Frame
		_ = conn.SetReadDeadline(deadline)
		if err := conn.ReadJSON(&out); err != nil {
			t.Fatalf("read: %v", err)
		}
		if out.Kind == protogen.EnvelopeKindEvent {
			continue
		}
		if out.ID == nil || *out.ID != id {
			t.Fatalf("id mismatch: got %v want %s", out.ID, id)
		}
		if out.Type != typ {
			t.Fatalf("type mismatch: got %q want %q", out.Type, typ)
		}
		return &out
	}
}

// --- tests ------------------------------------------------------------------

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
}

func TestUpgrade_MissingToken_401(t *testing.T) {
	h := newHarness(t)
	_, resp, err := h.dial(t, "")
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestUpgrade_BadSignature_401(t *testing.T) {
	h := newHarness(t)
	// Sign with a different key.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "rommel-backend", "sub": "u", "aud": "rommel-daemon",
		"wid": h.cfg.WID, "scope": []string{"fs:r"},
		"exp": now.Add(time.Minute).Unix(), "iat": now.Unix(), "jti": uuid.NewString(),
	}
	s, _ := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(otherPriv)

	_, resp, err := h.dial(t, s)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestUpgrade_WrongWID_401(t *testing.T) {
	h := newHarness(t)
	tok := h.mintToken(t, tokenOpts{wid: "some-other-workspace"})
	_, resp, err := h.dial(t, tok)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestUpgrade_ExpiredToken_401(t *testing.T) {
	h := newHarness(t)
	tok := h.mintToken(t, tokenOpts{exp: -time.Minute})
	_, resp, err := h.dial(t, tok)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %v want 401", resp)
	}
}

func TestPing_RoundTrip(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "system.ping", nil)
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want %q (err=%+v)", resp.Kind, protogen.EnvelopeKindResponse, resp.Error)
	}
}

func TestUnknownType_Errors(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.banana", nil)
	if resp.Kind != protogen.EnvelopeKindError {
		t.Fatalf("kind: got %q want error", resp.Kind)
	}
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeUnknownType {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeUnknownType)
	}
}

func TestFsRead_HappyPath_Utf8(t *testing.T) {
	h := newHarness(t)
	content := "hello, rommel\n"
	path := filepath.Join(h.root, "greeting.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "greeting.txt"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want response (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FsReadResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Contents != content {
		t.Fatalf("contents: got %q want %q", body.Contents, content)
	}
	if body.Size != len(content) {
		t.Fatalf("size: got %d want %d", body.Size, len(content))
	}
	if body.Encoding != protogen.FsReadResponseEncodingUtf8 {
		t.Fatalf("encoding: got %q want utf-8", body.Encoding)
	}
}

func TestFsRead_Base64Binary(t *testing.T) {
	h := newHarness(t)
	bin := []byte{0x00, 0xff, 0x01, 0x80, 0x7f}
	path := filepath.Join(h.root, "blob.bin")
	if err := os.WriteFile(path, bin, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "blob.bin", "encoding": "base64"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want response (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FsReadResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Encoding != protogen.FsReadResponseEncodingBase64 {
		t.Fatalf("encoding: got %q want base64", body.Encoding)
	}
}

func TestFsRead_AbsolutePath_Rejected(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "/etc/passwd"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsInvalidPath {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsInvalidPath)
	}
}

func TestFsRead_DotDotEscape_Rejected(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "../../etc/passwd"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsInvalidPath {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsInvalidPath)
	}
}

func TestFsRead_NotFound(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "no-such-file"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsNotFound {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsNotFound)
	}
}

// --- fs.list ----------------------------------------------------------------

func TestFsList_HappyPath(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(filepath.Join(h.root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.root, "b.txt"), []byte("bb"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Mkdir(filepath.Join(h.root, "sub"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.list", map[string]any{"path": "."})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want response (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FsListResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Entries) != 3 {
		t.Fatalf("entries: got %d want 3 (%+v)", len(body.Entries), body.Entries)
	}
	// sorted by name → a.txt, b.txt, sub
	if body.Entries[0].Name != "a.txt" || body.Entries[1].Name != "b.txt" || body.Entries[2].Name != "sub" {
		t.Fatalf("order: %+v", body.Entries)
	}
	if body.Entries[2].Kind != protogen.FsListEntryKindDir {
		t.Fatalf("kind: got %q want dir for sub", body.Entries[2].Kind)
	}
	if body.Entries[1].Size != 2 {
		t.Fatalf("size: got %d want 2 for b.txt", body.Entries[1].Size)
	}
}

func TestFsList_NotADir_Rejected(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(filepath.Join(h.root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.list", map[string]any{"path": "f"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsInvalidPath {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsInvalidPath)
	}
}

func TestFsList_NotFound(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.list", map[string]any{"path": "nope"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsNotFound {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsNotFound)
	}
}

// --- fs.write ---------------------------------------------------------------

func TestFsWrite_Creates(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.write", map[string]any{"path": "new.txt", "contents": "hello"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q want response (err=%+v)", resp.Kind, resp.Error)
	}
	got, err := os.ReadFile(filepath.Join(h.root, "new.txt"))
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("on-disk: got %q want %q", got, "hello")
	}
}

func TestFsWrite_Overwrites(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.root, "f.txt")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.write", map[string]any{"path": "f.txt", "contents": "second"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q (err=%+v)", resp.Kind, resp.Error)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Fatalf("on-disk: got %q want %q", got, "second")
	}
}

func TestFsWrite_Base64_Binary(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// base64 of bytes {0x00, 0xff, 0x10}
	resp := h.roundTrip(t, conn, "request", "fs.write", map[string]any{
		"path":     "blob.bin",
		"contents": "AP8Q",
		"encoding": "base64",
	})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q (err=%+v)", resp.Kind, resp.Error)
	}
	got, _ := os.ReadFile(filepath.Join(h.root, "blob.bin"))
	if len(got) != 3 || got[0] != 0x00 || got[1] != 0xff || got[2] != 0x10 {
		t.Fatalf("bytes: %v", got)
	}
}

func TestFsWrite_AbsolutePath_Rejected(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.write", map[string]any{"path": "/etc/passwd", "contents": "x"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsInvalidPath {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsInvalidPath)
	}
}

func TestFsWrite_InsufficientScope_Forbidden(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.write", map[string]any{"path": "x", "contents": "y"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeForbidden {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeForbidden)
	}
}

func TestFsWrite_ParentMissing_NotFound(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.write", map[string]any{
		"path":     "missing-dir/f.txt",
		"contents": "x",
	})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFsNotFound {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFsNotFound)
	}
}

// --- funnel.list ------------------------------------------------------------

// seedFunnel makes <root>/rommel/<stage>/ and drops files in it. Returns the
// stage dir path.
func seedFunnel(t *testing.T, root, stage string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, "rommel", stage)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	return dir
}

func TestFunnelList_HappyPath(t *testing.T) {
	h := newHarness(t)
	seedFunnel(t, h.root, "triage", map[string]string{
		"idea-a.md": "# A",
		"idea-b.md": "# B",
	})
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"funnel:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.list", map[string]any{"stage": "triage"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FunnelListResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(body.Stage) != "triage" {
		t.Fatalf("stage: got %q want triage", body.Stage)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("entries: got %d (%+v)", len(body.Entries), body.Entries)
	}
	if body.Entries[0].Name != "idea-a.md" || body.Entries[1].Name != "idea-b.md" {
		t.Fatalf("order: %+v", body.Entries)
	}
}

func TestFunnelList_MissingDir_ReturnsEmpty(t *testing.T) {
	// rommel/ does not exist — must return empty, not error.
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"funnel:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.list", map[string]any{"stage": "plans"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FunnelListResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Entries) != 0 {
		t.Fatalf("entries: got %d want 0", len(body.Entries))
	}
}

func TestFunnelList_InvalidStage_Rejected(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"funnel:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Bypass the request struct's JSON enum decoder by sending raw — we want
	// the handler-level rejection path tested, not the codegen's unmarshal.
	resp := h.roundTrip(t, conn, "request", "funnel.list", map[string]any{"stage": "nope"})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	// Either the bad_request from the protogen unmarshaler OR the
	// funnel.invalid_stage from the handler is acceptable. Both prove the
	// invalid stage didn't reach the filesystem.
	if resp.Error.Code != wsx.ErrCodeFunnelInvalidStage && resp.Error.Code != wsx.ErrCodeBadRequest {
		t.Fatalf("code: got %+v", resp.Error)
	}
}

// --- funnel.read ------------------------------------------------------------

func TestFunnelRead_HappyPath(t *testing.T) {
	h := newHarness(t)
	seedFunnel(t, h.root, "plans", map[string]string{"plan-x.md": "# Plan X\n\nbody"})
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"funnel:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.read", map[string]any{"stage": "plans", "name": "plan-x.md"})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q (err=%+v)", resp.Kind, resp.Error)
	}
	var body protogen.FunnelReadResponse
	if err := json.Unmarshal(resp.Payload, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Contents != "# Plan X\n\nbody" {
		t.Fatalf("contents: %q", body.Contents)
	}
}

func TestFunnelRead_InvalidName_Rejected(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"funnel:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.read", map[string]any{
		"stage": "plans",
		"name":  "../../etc/passwd",
	})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFunnelInvalidName {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFunnelInvalidName)
	}
}

func TestFunnelRead_NotFound(t *testing.T) {
	h := newHarness(t)
	// Make the stage dir exist so we get past the parent-dir check.
	seedFunnel(t, h.root, "triage", nil)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"funnel:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.read", map[string]any{"stage": "triage", "name": "absent.md"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFunnelNotFound {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFunnelNotFound)
	}
}

// --- funnel.promote ---------------------------------------------------------

func TestFunnelPromote_HappyPath(t *testing.T) {
	h := newHarness(t)
	seedFunnel(t, h.root, "triage", map[string]string{"card.md": "# card"})
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.promote", map[string]any{
		"name": "card.md", "from": "triage", "to": "plans",
	})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q (err=%+v)", resp.Kind, resp.Error)
	}
	// Old location gone, new location present.
	if _, err := os.Stat(filepath.Join(h.root, "rommel", "triage", "card.md")); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "rommel", "plans", "card.md")); err != nil {
		t.Fatalf("dest missing: %v", err)
	}
}

func TestFunnelPromote_BackwardsRejected(t *testing.T) {
	h := newHarness(t)
	seedFunnel(t, h.root, "plans", map[string]string{"card.md": "x"})
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.promote", map[string]any{
		"name": "card.md", "from": "plans", "to": "triage",
	})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFunnelInvalidTransition {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFunnelInvalidTransition)
	}
}

func TestFunnelPromote_ArchiveFromAnywhere(t *testing.T) {
	h := newHarness(t)
	seedFunnel(t, h.root, "triage", map[string]string{"oops.md": "kill me"})
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.promote", map[string]any{
		"name": "oops.md", "from": "triage", "to": "archive",
	})
	if resp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("kind: got %q (err=%+v)", resp.Kind, resp.Error)
	}
}

func TestFunnelPromote_NotFound(t *testing.T) {
	h := newHarness(t)
	seedFunnel(t, h.root, "triage", nil)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.promote", map[string]any{
		"name": "ghost.md", "from": "triage", "to": "plans",
	})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeFunnelNotFound {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeFunnelNotFound)
	}
}

func TestFunnel_InsufficientScope_Forbidden(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "funnel.list", map[string]any{"stage": "triage"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeForbidden {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeForbidden)
	}
}

func TestFsRead_InsufficientScope_Forbidden(t *testing.T) {
	h := newHarness(t)
	// Token has only pty:rw — no fs scope.
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "fs.read", map[string]any{"path": "x"})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeForbidden {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeForbidden)
	}
}

func TestBadEnvelope_BadRequest(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Skip the type field entirely.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"kind":"request"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out wsx.Frame
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.Error == nil || out.Error.Code != wsx.ErrCodeBadRequest {
		t.Fatalf("code: got %+v want %q", out.Error, wsx.ErrCodeBadRequest)
	}
}

// --- pty.* ------------------------------------------------------------------

// drainUntil reads frames from conn until predicate returns true, the
// connection errors, or the timeout elapses. Returns the matched frame and
// every frame seen along the way (useful for cumulative assertions like
// "did 'rommel' appear anywhere in the output stream?").
func drainUntil(t *testing.T, conn *websocket.Conn, timeout time.Duration, pred func(*wsx.Frame) bool) (*wsx.Frame, []*wsx.Frame) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var seen []*wsx.Frame
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, seen
		}
		_ = conn.SetReadDeadline(time.Now().Add(remaining))
		var f wsx.Frame
		if err := conn.ReadJSON(&f); err != nil {
			return nil, seen
		}
		fc := f
		seen = append(seen, &fc)
		if pred(&fc) {
			return &fc, seen
		}
	}
}

// sendFrame writes a request frame; doesn't wait for a response.
func sendFrame(t *testing.T, conn *websocket.Conn, typ string, payload any) string {
	t.Helper()
	id := uuid.NewString()
	raw, _ := json.Marshal(payload)
	frame := wsx.Frame{
		Kind:    protogen.EnvelopeKindRequest,
		Type:    typ,
		ID:      &id,
		Payload: raw,
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	return id
}

func TestPty_OpenAndExit(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	openResp := h.roundTrip(t, conn, "request", "pty.open", map[string]any{"cols": 80, "rows": 24})
	if openResp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("open kind: %q (err=%+v)", openResp.Kind, openResp.Error)
	}
	var open protogen.PtyOpenResponse
	if err := json.Unmarshal(openResp.Payload, &open); err != nil {
		t.Fatalf("unmarshal open: %v", err)
	}
	if _, err := uuid.Parse(open.PtyID); err != nil {
		t.Fatalf("pty_id not a UUID: %q", open.PtyID)
	}

	// Type "exit 0\n" → shell exits → pty.exit fires with exit_code 0.
	sendFrame(t, conn, "pty.input", map[string]any{
		"pty_id": open.PtyID,
		"data":   base64.StdEncoding.EncodeToString([]byte("exit 0\n")),
	})

	exit, _ := drainUntil(t, conn, 5*time.Second, func(f *wsx.Frame) bool {
		return f.Kind == protogen.EnvelopeKindEvent && f.Type == "pty.exit"
	})
	if exit == nil {
		t.Fatalf("never saw pty.exit")
	}
	var ev protogen.PtyExitEvent
	if err := json.Unmarshal(exit.Payload, &ev); err != nil {
		t.Fatalf("unmarshal exit: %v", err)
	}
	if ev.PtyID != open.PtyID {
		t.Fatalf("exit pty_id: got %q want %q", ev.PtyID, open.PtyID)
	}
	if ev.ExitCode != 0 {
		t.Fatalf("exit_code: got %d want 0 (signal=%v)", ev.ExitCode, ev.Signal)
	}
}

func TestPty_InputProducesOutput(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	openResp := h.roundTrip(t, conn, "request", "pty.open", map[string]any{"cols": 80, "rows": 24})
	var open protogen.PtyOpenResponse
	_ = json.Unmarshal(openResp.Payload, &open)

	// "echo rommel-magic\nexit 0\n" — the shell echoes the command (because
	// the PTY is in echo-mode) AND prints the output. Both contain
	// rommel-magic, so we can sanity-check decoded data without fighting
	// prompt noise.
	sendFrame(t, conn, "pty.input", map[string]any{
		"pty_id": open.PtyID,
		"data":   base64.StdEncoding.EncodeToString([]byte("echo rommel-magic\nexit 0\n")),
	})

	var accum strings.Builder
	_, frames := drainUntil(t, conn, 5*time.Second, func(f *wsx.Frame) bool {
		if f.Kind == protogen.EnvelopeKindEvent && f.Type == "pty.output" {
			var oe protogen.PtyOutputEvent
			if json.Unmarshal(f.Payload, &oe) == nil {
				if b, err := base64.StdEncoding.DecodeString(oe.Data); err == nil {
					accum.Write(b)
				}
			}
		}
		return f.Kind == protogen.EnvelopeKindEvent && f.Type == "pty.exit"
	})
	if !strings.Contains(accum.String(), "rommel-magic") {
		t.Fatalf("output never contained 'rommel-magic'; saw %d frames, accum=%q", len(frames), accum.String())
	}
}

func TestPty_ResizeRoundTrip(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	openResp := h.roundTrip(t, conn, "request", "pty.open", map[string]any{"cols": 80, "rows": 24})
	var open protogen.PtyOpenResponse
	_ = json.Unmarshal(openResp.Payload, &open)

	resizeResp := h.roundTrip(t, conn, "request", "pty.resize", map[string]any{
		"pty_id": open.PtyID, "cols": 120, "rows": 40,
	})
	if resizeResp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("resize kind: %q (err=%+v)", resizeResp.Kind, resizeResp.Error)
	}
	if string(resizeResp.Payload) != `{}` {
		t.Fatalf("resize payload: got %q want {}", resizeResp.Payload)
	}
}

func TestPty_ResizeInvalidSize(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	openResp := h.roundTrip(t, conn, "request", "pty.open", map[string]any{"cols": 80, "rows": 24})
	var open protogen.PtyOpenResponse
	_ = json.Unmarshal(openResp.Payload, &open)

	// 9999 > 1000 → invalid_size from codegen's UnmarshalJSON validator;
	// daemon would surface invalid_size if codegen let it through.
	resp := h.roundTrip(t, conn, "request", "pty.resize", map[string]any{
		"pty_id": open.PtyID, "cols": 9999, "rows": 24,
	})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != wsx.ErrCodePtyInvalidSize && resp.Error.Code != wsx.ErrCodeBadRequest {
		t.Fatalf("code: got %+v want pty.invalid_size or bad_request", resp.Error)
	}
}

func TestPty_CloseIsIdempotent(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	openResp := h.roundTrip(t, conn, "request", "pty.open", map[string]any{"cols": 80, "rows": 24})
	var open protogen.PtyOpenResponse
	_ = json.Unmarshal(openResp.Payload, &open)

	r1 := h.roundTrip(t, conn, "request", "pty.close", map[string]any{"pty_id": open.PtyID})
	if r1.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("first close: %q (err=%+v)", r1.Kind, r1.Error)
	}
	// Second close: idempotent — same success even though the PTY is gone.
	r2 := h.roundTrip(t, conn, "request", "pty.close", map[string]any{"pty_id": open.PtyID})
	if r2.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("second close: %q (err=%+v)", r2.Kind, r2.Error)
	}
	// Unknown id is also idempotent success.
	r3 := h.roundTrip(t, conn, "request", "pty.close", map[string]any{"pty_id": uuid.NewString()})
	if r3.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("unknown close: %q (err=%+v)", r3.Kind, r3.Error)
	}
}

func TestPty_InputUnknownPtyId(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// pty.input is fire-and-forget on success; on error we still get an
	// error envelope correlated by request id.
	resp := h.roundTrip(t, conn, "request", "pty.input", map[string]any{
		"pty_id": uuid.NewString(),
		"data":   base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodePtyNotFound {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodePtyNotFound)
	}
}

func TestPty_InsufficientScope(t *testing.T) {
	h := newHarness(t)
	// fs:r only; no pty:rw.
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"fs:r"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	resp := h.roundTrip(t, conn, "request", "pty.open", map[string]any{"cols": 80, "rows": 24})
	if resp.Error == nil || resp.Error.Code != wsx.ErrCodeForbidden {
		t.Fatalf("code: got %+v want %q", resp.Error, wsx.ErrCodeForbidden)
	}
}

func TestPty_OnDisconnectCleansUp(t *testing.T) {
	h := newHarness(t)
	conn, _, err := h.dial(t, h.mintToken(t, tokenOpts{scope: []string{"pty:rw"}}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	openResp := h.roundTrip(t, conn, "request", "pty.open", map[string]any{"cols": 80, "rows": 24})
	if openResp.Kind != protogen.EnvelopeKindResponse {
		t.Fatalf("open: %+v", openResp.Error)
	}

	// Drop the WS — runConn's deferred cleanup calls OnDisconnect, which
	// SIGTERMs every session owned by this conn.
	conn.Close()

	// Allow OnDisconnect + the SIGTERM/grace/SIGKILL chain to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// Internal field access via the harness handle. Tests in this
		// package are inside the same module; the helper is intentionally
		// minimal so the public surface stays narrow.
		if remaining := h.pty.OpenSessionCount(); remaining == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("OnDisconnect did not clean up; %d sessions still open", h.pty.OpenSessionCount())
}

// sanity: make sure protogen envelope enum string we encode actually matches.
func TestEnvelopeKindStrings(t *testing.T) {
	if string(protogen.EnvelopeKindRequest) != "request" {
		t.Fatalf("envelope kind drift: %q", protogen.EnvelopeKindRequest)
	}
	if !strings.HasPrefix(fmt.Sprint(protogen.EnvelopeKindError), "error") {
		t.Fatalf("envelope kind drift: %q", protogen.EnvelopeKindError)
	}
}
