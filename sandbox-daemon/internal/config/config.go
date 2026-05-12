// Package config loads daemon configuration from environment variables.
//
// All required vars are validated up front so the daemon fails fast at startup
// rather than mid-request. The set of required vars is intentionally tiny
// (workspace root, workspace id, token public key) so the workspace VM image
// can populate them from a single entrypoint script.
package config

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type Config struct {
	Port          int
	WorkspaceRoot string
	WID           string
	TokenPublic   ed25519.PublicKey
}

// FromEnv reads ROMMEL_* env vars and returns a populated Config or an error
// listing every missing/invalid var (not just the first).
func FromEnv() (*Config, error) {
	var errs []string

	port := 7777
	if raw := os.Getenv("ROMMEL_PORT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 65535 {
			errs = append(errs, fmt.Sprintf("ROMMEL_PORT=%q is not a valid TCP port", raw))
		} else {
			port = n
		}
	}

	root := os.Getenv("ROMMEL_WORKSPACE_ROOT")
	if root == "" {
		errs = append(errs, "ROMMEL_WORKSPACE_ROOT is required")
	} else {
		abs, err := filepath.Abs(root)
		if err != nil {
			errs = append(errs, fmt.Sprintf("ROMMEL_WORKSPACE_ROOT=%q: %v", root, err))
		} else {
			info, err := os.Stat(abs)
			switch {
			case err != nil:
				errs = append(errs, fmt.Sprintf("ROMMEL_WORKSPACE_ROOT=%q: %v", abs, err))
			case !info.IsDir():
				errs = append(errs, fmt.Sprintf("ROMMEL_WORKSPACE_ROOT=%q is not a directory", abs))
			default:
				root = abs
			}
		}
	}

	wid := os.Getenv("ROMMEL_WID")
	if wid == "" {
		errs = append(errs, "ROMMEL_WID is required")
	}

	pemStr := os.Getenv("ROMMEL_TOKEN_PUBKEY")
	var pub ed25519.PublicKey
	if pemStr == "" {
		errs = append(errs, "ROMMEL_TOKEN_PUBKEY is required (Ed25519 PEM)")
	} else {
		key, err := jwt.ParseEdPublicKeyFromPEM([]byte(pemStr))
		if err != nil {
			errs = append(errs, fmt.Sprintf("ROMMEL_TOKEN_PUBKEY: %v", err))
		} else {
			edPub, ok := key.(ed25519.PublicKey)
			if !ok {
				errs = append(errs, "ROMMEL_TOKEN_PUBKEY: not an Ed25519 public key")
			} else {
				pub = edPub
			}
		}
	}

	if len(errs) > 0 {
		return nil, errors.New("config: " + strings.Join(errs, "; "))
	}

	return &Config{
		Port:          port,
		WorkspaceRoot: root,
		WID:           wid,
		TokenPublic:   pub,
	}, nil
}
