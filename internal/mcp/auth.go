package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ExtractUserFromToken extracts the user identity (email or subject) from a
// JWT token forwarded by muster. This does not verify the token -- verification
// is handled by muster before forwarding.
func ExtractUserFromToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("no token provided")
	}

	// Strip "Bearer " prefix.
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")

	// JWT has three parts separated by dots.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode the payload (second part).
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decoding JWT payload: %w", err)
	}

	// Parse claims.
	var claims struct {
		Email   string `json:"email"`
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parsing JWT claims: %w", err)
	}

	// Prefer email, fall back to subject.
	if claims.Email != "" {
		return claims.Email, nil
	}
	if claims.Subject != "" {
		return claims.Subject, nil
	}

	return "", fmt.Errorf("JWT contains neither email nor sub claim")
}
