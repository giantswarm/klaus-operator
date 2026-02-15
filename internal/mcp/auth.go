package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// contextKey is a private type for context keys in this package.
type contextKey int

const (
	// authTokenKey is the context key for the Authorization header value.
	authTokenKey contextKey = iota
)

// HTTPContextFuncAuth extracts the Authorization header from the incoming HTTP
// request and stores it in the context. It is used with mcp-go's
// WithHTTPContextFunc to make the token available to tool handlers.
func HTTPContextFuncAuth(ctx context.Context, r *http.Request) context.Context {
	if token := r.Header.Get("Authorization"); token != "" {
		ctx = context.WithValue(ctx, authTokenKey, token)
	}
	return ctx
}

// AuthTokenFromContext retrieves the Authorization header value stored in the
// context by HTTPContextFuncAuth.
func AuthTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(authTokenKey).(string); ok {
		return token
	}
	return ""
}

// ExtractUserFromToken extracts the user identity (email or subject) from a
// JWT token forwarded by muster. This does not verify the token -- verification
// is handled by muster before forwarding.
func ExtractUserFromToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("no token provided")
	}

	// Strip "Bearer " prefix (case-insensitive per RFC 6750).
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = token[7:]
	}

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
