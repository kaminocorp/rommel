package config_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rommel-ade/rommel/sandbox-daemon/internal/config"
)

func TestFromEnv_HappyPath(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	der, _ := x509.MarshalPKIXPublicKey(pub)
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	root := t.TempDir()
	t.Setenv("ROMMEL_WORKSPACE_ROOT", root)
	t.Setenv("ROMMEL_WID", "wid-1")
	t.Setenv("ROMMEL_TOKEN_PUBKEY", pemStr)
	t.Setenv("ROMMEL_PORT", "9090")

	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Port != 9090 {
		t.Fatalf("port: got %d", cfg.Port)
	}
	if cfg.WID != "wid-1" {
		t.Fatalf("wid: got %q", cfg.WID)
	}
	if cfg.WorkspaceRoot != filepath.Clean(root) {
		t.Fatalf("root: got %q want %q", cfg.WorkspaceRoot, root)
	}
	if len(cfg.TokenPublic) != ed25519.PublicKeySize {
		t.Fatalf("pubkey size: got %d", len(cfg.TokenPublic))
	}
}

func TestFromEnv_MissingAllRequired(t *testing.T) {
	os.Unsetenv("ROMMEL_WORKSPACE_ROOT")
	os.Unsetenv("ROMMEL_WID")
	os.Unsetenv("ROMMEL_TOKEN_PUBKEY")
	os.Unsetenv("ROMMEL_PORT")

	_, err := config.FromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"ROMMEL_WORKSPACE_ROOT", "ROMMEL_WID", "ROMMEL_TOKEN_PUBKEY"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing mention of %s: %s", want, msg)
		}
	}
}

func TestFromEnv_RootNotDir(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(pub)
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("ROMMEL_WORKSPACE_ROOT", f)
	t.Setenv("ROMMEL_WID", "wid")
	t.Setenv("ROMMEL_TOKEN_PUBKEY", pemStr)

	if _, err := config.FromEnv(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("error: %v", err)
	}
}
