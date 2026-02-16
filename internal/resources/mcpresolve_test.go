package resources

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestServerConfigToRawExtension_HTTP(t *testing.T) {
	spec := &klausv1alpha1.KlausMCPServerSpec{
		Type: "http",
		URL:  "https://api.example.com/mcp/",
		Headers: map[string]string{
			"Authorization": "Bearer ${TOKEN}",
		},
		SecretRefs: []klausv1alpha1.MCPServerSecret{
			{SecretName: "my-secret", Env: map[string]string{"TOKEN": "token"}},
		},
	}

	ext, err := ServerConfigToRawExtension(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(ext.Raw, &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if config["type"] != "http" {
		t.Errorf("expected type=http, got %v", config["type"])
	}
	if config["url"] != "https://api.example.com/mcp/" {
		t.Errorf("expected url, got %v", config["url"])
	}

	headers, ok := config["headers"].(map[string]any)
	if !ok {
		t.Fatal("expected headers map")
	}
	if headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Errorf("expected Authorization header, got %v", headers["Authorization"])
	}

	// SecretRefs should NOT be in the config (used for pod env only).
	if _, exists := config["secretRefs"]; exists {
		t.Error("secretRefs should not be included in the server config JSON")
	}
}

func TestServerConfigToRawExtension_Stdio(t *testing.T) {
	spec := &klausv1alpha1.KlausMCPServerSpec{
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "@bytebase/dbhub"},
		Env: map[string]string{
			"DB_URL": "${DATABASE_URL}",
		},
	}

	ext, err := ServerConfigToRawExtension(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(ext.Raw, &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if config["type"] != "stdio" {
		t.Errorf("expected type=stdio, got %v", config["type"])
	}
	if config["command"] != "npx" {
		t.Errorf("expected command=npx, got %v", config["command"])
	}

	args, ok := config["args"].([]any)
	if !ok || len(args) != 2 {
		t.Errorf("expected 2 args, got %v", config["args"])
	}
}

func TestServerConfigToRawExtension_EmptyOptionalFields(t *testing.T) {
	spec := &klausv1alpha1.KlausMCPServerSpec{
		Type: "http",
		URL:  "https://example.com/mcp/",
	}

	ext, err := ServerConfigToRawExtension(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(ext.Raw, &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	// Only type and url should be present.
	if len(config) != 2 {
		t.Errorf("expected 2 fields, got %d: %v", len(config), config)
	}
}

func TestMergeResolvedMCPIntoInstance_ServerConfigs(t *testing.T) {
	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			MCPServers: map[string]runtime.RawExtension{
				"inline-tool": {
					Raw: json.RawMessage(`{"type":"stdio","command":"inline-cmd"}`),
				},
				"shared-tool": {
					Raw: json.RawMessage(`{"type":"http","url":"https://inline.example.com"}`),
				},
			},
		},
	}

	resolved := &ResolvedMCPConfig{
		Servers: map[string]runtime.RawExtension{
			"shared-tool": {
				Raw: json.RawMessage(`{"type":"http","url":"https://resolved.example.com"}`),
			},
			"new-tool": {
				Raw: json.RawMessage(`{"type":"stdio","command":"new-cmd"}`),
			},
		},
	}

	MergeResolvedMCPIntoInstance(resolved, instance)

	// Should have 3 servers total.
	if len(instance.Claude.MCPServers) != 3 {
		t.Errorf("expected 3 MCP servers, got %d", len(instance.Claude.MCPServers))
	}

	// inline-tool should be unchanged.
	if string(instance.Claude.MCPServers["inline-tool"].Raw) != `{"type":"stdio","command":"inline-cmd"}` {
		t.Errorf("inline-tool should be unchanged, got %s", instance.Claude.MCPServers["inline-tool"].Raw)
	}

	// shared-tool should be overridden by resolved.
	if string(instance.Claude.MCPServers["shared-tool"].Raw) != `{"type":"http","url":"https://resolved.example.com"}` {
		t.Errorf("shared-tool should be overridden by resolved, got %s", instance.Claude.MCPServers["shared-tool"].Raw)
	}

	// new-tool should be added.
	if string(instance.Claude.MCPServers["new-tool"].Raw) != `{"type":"stdio","command":"new-cmd"}` {
		t.Errorf("new-tool should be added, got %s", instance.Claude.MCPServers["new-tool"].Raw)
	}
}

func TestMergeResolvedMCPIntoInstance_NilResolved(t *testing.T) {
	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			MCPServers: map[string]runtime.RawExtension{
				"inline": {Raw: json.RawMessage(`{"type":"http"}`)},
			},
		},
	}

	MergeResolvedMCPIntoInstance(nil, instance)

	if len(instance.Claude.MCPServers) != 1 {
		t.Errorf("nil resolved should not modify instance, got %d servers", len(instance.Claude.MCPServers))
	}
}

func TestMergeResolvedMCPIntoInstance_EmptyInlineMCPServers(t *testing.T) {
	instance := &klausv1alpha1.KlausInstanceSpec{}

	resolved := &ResolvedMCPConfig{
		Servers: map[string]runtime.RawExtension{
			"new-tool": {Raw: json.RawMessage(`{"type":"http","url":"https://example.com"}`)},
		},
	}

	MergeResolvedMCPIntoInstance(resolved, instance)

	if len(instance.Claude.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(instance.Claude.MCPServers))
	}
}

func TestDeduplicateMCPServerSecrets_NoConflict(t *testing.T) {
	inline := []klausv1alpha1.MCPServerSecret{
		{SecretName: "secret-a", Env: map[string]string{"TOKEN_A": "key-a"}},
	}
	resolved := []klausv1alpha1.MCPServerSecret{
		{SecretName: "secret-b", Env: map[string]string{"TOKEN_B": "key-b"}},
	}

	result := DeduplicateMCPServerSecrets(inline, resolved)

	// Should have 2 secrets (no conflict).
	if len(result) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(result))
	}

	// Verify both env vars are present.
	allEnvs := collectEnvVars(result)
	if _, ok := allEnvs["TOKEN_A"]; !ok {
		t.Error("TOKEN_A should be present")
	}
	if _, ok := allEnvs["TOKEN_B"]; !ok {
		t.Error("TOKEN_B should be present")
	}
}

func TestDeduplicateMCPServerSecrets_ResolvedWins(t *testing.T) {
	inline := []klausv1alpha1.MCPServerSecret{
		{SecretName: "inline-secret", Env: map[string]string{"TOKEN": "inline-key"}},
	}
	resolved := []klausv1alpha1.MCPServerSecret{
		{SecretName: "resolved-secret", Env: map[string]string{"TOKEN": "resolved-key"}},
	}

	result := DeduplicateMCPServerSecrets(inline, resolved)

	allEnvs := collectEnvVars(result)
	ref, ok := allEnvs["TOKEN"]
	if !ok {
		t.Fatal("TOKEN should be present")
	}

	// Resolved should win.
	if ref.secretName != "resolved-secret" || ref.key != "resolved-key" {
		t.Errorf("resolved should win: expected resolved-secret/resolved-key, got %s/%s",
			ref.secretName, ref.key)
	}
}

func TestDeduplicateMCPServerSecrets_Empty(t *testing.T) {
	result := DeduplicateMCPServerSecrets(nil, nil)
	if result != nil {
		t.Errorf("expected nil for empty inputs, got %v", result)
	}
}

func TestDeduplicateMCPServerSecrets_DeterministicOrder(t *testing.T) {
	inline := []klausv1alpha1.MCPServerSecret{
		{SecretName: "secret-z", Env: map[string]string{"Z_VAR": "z-key"}},
	}
	resolved := []klausv1alpha1.MCPServerSecret{
		{SecretName: "secret-a", Env: map[string]string{"A_VAR": "a-key"}},
	}

	// Run multiple times to verify deterministic ordering.
	for i := 0; i < 10; i++ {
		result := DeduplicateMCPServerSecrets(inline, resolved)
		if len(result) != 2 {
			t.Fatalf("expected 2 secrets, got %d", len(result))
		}
		// Sorted by secret name: secret-a before secret-z.
		if result[0].SecretName != "secret-a" {
			t.Errorf("iteration %d: expected secret-a first, got %s", i, result[0].SecretName)
		}
		if result[1].SecretName != "secret-z" {
			t.Errorf("iteration %d: expected secret-z second, got %s", i, result[1].SecretName)
		}
	}
}

type secretRef struct {
	secretName string
	key        string
}

func collectEnvVars(secrets []klausv1alpha1.MCPServerSecret) map[string]secretRef {
	result := make(map[string]secretRef)
	for _, s := range secrets {
		for envVar, key := range s.Env {
			result[envVar] = secretRef{secretName: s.SecretName, key: key}
		}
	}
	return result
}
