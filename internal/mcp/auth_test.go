package mcp

import (
	"context"
	"encoding/base64"
	"net/http"
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
			name:     "with lowercase bearer prefix",
			token:    "bearer " + buildTestJWT(`{"email":"admin@test.io"}`),
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

func TestHTTPContextFuncAuth(t *testing.T) {
	token := "Bearer " + buildTestJWT(`{"email":"user@example.com"}`)

	t.Run("injects token into context", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", token)

		ctx := HTTPContextFuncAuth(context.Background(), req)

		got := AuthTokenFromContext(ctx)
		if got != token {
			t.Errorf("AuthTokenFromContext() = %q, want %q", got, token)
		}
	})

	t.Run("no auth header returns empty", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/mcp", nil)

		ctx := HTTPContextFuncAuth(context.Background(), req)

		got := AuthTokenFromContext(ctx)
		if got != "" {
			t.Errorf("AuthTokenFromContext() = %q, want empty", got)
		}
	})

	t.Run("empty context returns empty", func(t *testing.T) {
		got := AuthTokenFromContext(context.Background())
		if got != "" {
			t.Errorf("AuthTokenFromContext() = %q, want empty", got)
		}
	})
}

// buildTestJWT creates a minimal JWT with the given payload.
// Header and signature are dummy values -- we only decode the payload.
func buildTestJWT(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return strings.Join([]string{header, body, sig}, ".")
}
