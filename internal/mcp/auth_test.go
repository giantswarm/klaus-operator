package mcp

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestExtractUserFromToken(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		wantUser  string
		wantError bool
	}{
		{
			name:      "empty token",
			token:     "",
			wantError: true,
		},
		{
			name:      "invalid format",
			token:     "not-a-jwt",
			wantError: true,
		},
		{
			name:     "email claim",
			token:    buildTestJWT(`{"email":"user@example.com","sub":"user123"}`),
			wantUser: "user@example.com",
		},
		{
			name:     "sub claim only",
			token:    buildTestJWT(`{"sub":"user456"}`),
			wantUser: "user456",
		},
		{
			name:     "with bearer prefix",
			token:    "Bearer " + buildTestJWT(`{"email":"admin@test.io"}`),
			wantUser: "admin@test.io",
		},
		{
			name:      "no email or sub",
			token:     buildTestJWT(`{"name":"test"}`),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := ExtractUserFromToken(tt.token)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got user: %q", user)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if user != tt.wantUser {
				t.Errorf("ExtractUserFromToken() = %q, want %q", user, tt.wantUser)
			}
		})
	}
}

// buildTestJWT creates a minimal JWT with the given payload.
// Header and signature are dummy values -- we only decode the payload.
func buildTestJWT(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return strings.Join([]string{header, body, sig}, ".")
}
