// Package auth verifies the EdDSA-signed broker tokens minted by the backend
// (POST /workspaces/:id/sessions) and produces a typed
// protogen.SessionTokenClaims that the rest of the daemon dispatches against.
//
// Verification rules:
//   - Signing method MUST be EdDSA (`alg=EdDSA`). All others rejected to avoid
//     the classic alg=none / RS256-vs-HS256 confusion attacks.
//   - `iss` == "rommel-backend"
//   - `aud` == "rommel-daemon"
//   - `wid` == this daemon's workspace id (rejects tokens minted for a peer)
//   - `exp` in the future (enforced by jwt/v5's default claim validation)
//   - All required claims present (enforced by protogen.SessionTokenClaims'
//     generated UnmarshalJSON; scope enum membership enforced there too).
package auth

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	protogen "github.com/rommel-ade/rommel/proto/clients/go/gen"
)

const (
	expectedIssuer   = "rommel-backend"
	expectedAudience = "rommel-daemon"
)

// ErrInvalidToken is returned for any failure during token verification.
// Callers translate this into a 401 on the WS upgrade.
var ErrInvalidToken = errors.New("invalid session token")

// Verify parses and validates a JWT, returning the typed claims on success.
// `pub` is the Ed25519 public key (typically Config.TokenPublic);
// `expectedWID` is this daemon's workspace id (Config.WID).
func Verify(tokenString string, pub ed25519.PublicKey, expectedWID string) (*protogen.SessionTokenClaims, error) {
	parsed, err := jwt.Parse(
		tokenString,
		func(t *jwt.Token) (interface{}, error) { return pub, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(expectedIssuer),
		jwt.WithAudience(expectedAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}

	// Re-marshal the claims through the protogen type so we get the generated
	// validation hooks (required fields + scope enum membership) for free.
	raw, err := json.Marshal(parsed.Claims)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling claims: %v", ErrInvalidToken, err)
	}
	var claims protogen.SessionTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if claims.Wid != expectedWID {
		return nil, fmt.Errorf("%w: wid %q does not match this daemon (%q)", ErrInvalidToken, claims.Wid, expectedWID)
	}

	return &claims, nil
}

// HasAnyScope returns true if claims carry at least one of `required`. Pass an
// empty `required` for primitives that need no scope (workspace.info, ping).
func HasAnyScope(claims *protogen.SessionTokenClaims, required ...protogen.SessionTokenClaimsScopeElem) bool {
	if len(required) == 0 {
		return true
	}
	for _, want := range required {
		for _, got := range claims.Scope {
			if got == want {
				return true
			}
		}
	}
	return false
}
