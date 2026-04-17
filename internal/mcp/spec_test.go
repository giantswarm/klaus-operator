package mcp

import (
	"testing"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestParsePluginReference(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		want    klausv1alpha1.PluginReference
		wantErr bool
	}{
		{
			name: "tag reference",
			ref:  "gsoci.azurecr.io/giantswarm/plugins/code-reviewer:v0.1.0",
			want: klausv1alpha1.PluginReference{
				Repository: "gsoci.azurecr.io/giantswarm/plugins/code-reviewer",
				Tag:        "v0.1.0",
			},
		},
		{
			name: "latest tag",
			ref:  "registry.example.com/plugin:latest",
			want: klausv1alpha1.PluginReference{
				Repository: "registry.example.com/plugin",
				Tag:        "latest",
			},
		},
		{
			name: "digest reference",
			ref:  "gsoci.azurecr.io/giantswarm/plugins/test@sha256:abc123def456",
			want: klausv1alpha1.PluginReference{
				Repository: "gsoci.azurecr.io/giantswarm/plugins/test",
				Digest:     "sha256:abc123def456",
			},
		},
		{
			name: "registry with port and tag",
			ref:  "localhost:5000/my-plugin:v1.0",
			want: klausv1alpha1.PluginReference{
				Repository: "localhost:5000/my-plugin",
				Tag:        "v1.0",
			},
		},
		{
			name:    "empty string",
			ref:     "",
			wantErr: true,
		},
		{
			name:    "no tag or digest",
			ref:     "gsoci.azurecr.io/giantswarm/plugins/code-reviewer",
			wantErr: true,
		},
		{
			name:    "bare host:port without path",
			ref:     "localhost:5000",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			ref:     "   ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePluginReference(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePluginReference(%q) = %+v, want error", tt.ref, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePluginReference(%q) unexpected error: %v", tt.ref, err)
			}
			if got.Repository != tt.want.Repository {
				t.Errorf("Repository = %q, want %q", got.Repository, tt.want.Repository)
			}
			if got.Tag != tt.want.Tag {
				t.Errorf("Tag = %q, want %q", got.Tag, tt.want.Tag)
			}
			if got.Digest != tt.want.Digest {
				t.Errorf("Digest = %q, want %q", got.Digest, tt.want.Digest)
			}
		})
	}
}

func TestBuildInstanceSpec_Defaults(t *testing.T) {
	args := map[string]any{}
	spec, err := buildInstanceSpec(args, "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.Owner != "user@example.com" {
		t.Errorf("Owner = %q, want %q", spec.Owner, "user@example.com")
	}
	if spec.Claude.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want default", spec.Claude.Model)
	}
	if spec.Claude.PermissionMode != klausv1alpha1.PermissionModeBypass {
		t.Errorf("PermissionMode = %q, want %q", spec.Claude.PermissionMode, klausv1alpha1.PermissionModeBypass)
	}
	if spec.Workspace != nil {
		t.Error("Workspace should be nil when no workspace params provided")
	}
	if len(spec.Plugins) != 0 {
		t.Error("Plugins should be empty when not provided")
	}
}

func TestBuildInstanceSpec_AllHighPriority(t *testing.T) {
	args := map[string]any{
		"model":                   "claude-opus-4-20250514",
		"system_prompt":           "You are a helpful assistant",
		"personality":             "gsoci.azurecr.io/giantswarm/personalities/go-dev:latest",
		"image":                   "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		"plugins":                 []any{"gsoci.azurecr.io/giantswarm/plugins/code-reviewer:v0.1.0", "gsoci.azurecr.io/giantswarm/plugins/test:v0.2.0"},
		"workspace_git_repo":      "https://github.com/giantswarm/my-repo.git",
		"workspace_git_ref":       "main",
		"workspace_git_secret":    "my-git-token",
		"workspace_storage_class": "premium-rwo",
		"workspace_size":          "10Gi",
		"max_budget_usd":          float64(5.0),
		"permission_mode":         "default",
		"max_turns":               float64(50),
		"effort":                  "high",
	}

	spec, err := buildInstanceSpec(args, "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.Claude.Model != "claude-opus-4-20250514" {
		t.Errorf("Model = %q, want %q", spec.Claude.Model, "claude-opus-4-20250514")
	}
	if spec.Claude.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("SystemPrompt = %q", spec.Claude.SystemPrompt)
	}
	if spec.Personality != "gsoci.azurecr.io/giantswarm/personalities/go-dev:latest" {
		t.Errorf("Personality = %q", spec.Personality)
	}
	if spec.Image != "gsoci.azurecr.io/giantswarm/klaus-go:1.25" {
		t.Errorf("Image = %q", spec.Image)
	}
	if spec.Claude.PermissionMode != klausv1alpha1.PermissionModeDefault {
		t.Errorf("PermissionMode = %q, want %q", spec.Claude.PermissionMode, klausv1alpha1.PermissionModeDefault)
	}
	if spec.Claude.MaxBudgetUSD == nil || *spec.Claude.MaxBudgetUSD != 5.0 {
		t.Errorf("MaxBudgetUSD = %v, want 5.0", spec.Claude.MaxBudgetUSD)
	}
	if spec.Claude.MaxTurns == nil || *spec.Claude.MaxTurns != 50 {
		t.Errorf("MaxTurns = %v, want 50", spec.Claude.MaxTurns)
	}
	if spec.Claude.Effort != klausv1alpha1.EffortHigh {
		t.Errorf("Effort = %q, want %q", spec.Claude.Effort, klausv1alpha1.EffortHigh)
	}

	// Plugins.
	if len(spec.Plugins) != 2 {
		t.Fatalf("Plugins count = %d, want 2", len(spec.Plugins))
	}
	if spec.Plugins[0].Repository != "gsoci.azurecr.io/giantswarm/plugins/code-reviewer" || spec.Plugins[0].Tag != "v0.1.0" {
		t.Errorf("Plugin[0] = %+v", spec.Plugins[0])
	}
	if spec.Plugins[1].Repository != "gsoci.azurecr.io/giantswarm/plugins/test" || spec.Plugins[1].Tag != "v0.2.0" {
		t.Errorf("Plugin[1] = %+v", spec.Plugins[1])
	}

	// Workspace.
	if spec.Workspace == nil {
		t.Fatal("Workspace should not be nil")
	}
	if spec.Workspace.GitRepo != "https://github.com/giantswarm/my-repo.git" {
		t.Errorf("GitRepo = %q", spec.Workspace.GitRepo)
	}
	if spec.Workspace.GitRef != "main" {
		t.Errorf("GitRef = %q", spec.Workspace.GitRef)
	}
	if spec.Workspace.GitSecretRef == nil || spec.Workspace.GitSecretRef.Name != "my-git-token" {
		t.Errorf("GitSecretRef = %+v", spec.Workspace.GitSecretRef)
	}
	if spec.Workspace.StorageClass != "premium-rwo" {
		t.Errorf("StorageClass = %q", spec.Workspace.StorageClass)
	}
	if spec.Workspace.Size == nil || spec.Workspace.Size.String() != "10Gi" {
		t.Errorf("Size = %v", spec.Workspace.Size)
	}
}

func TestBuildInstanceSpec_MediumPriority(t *testing.T) {
	args := map[string]any{
		"append_system_prompt": "Always respond in Japanese",
		"fallback_model":       "claude-haiku-4-5-20251001",
		"mode":                 "chat",
		"allowed_tools":        []any{"Read", "Write", "Bash"},
		"disallowed_tools":     []any{"WebSearch"},
		"mcp_servers":          []any{"github-server", "slack-server"},
	}

	spec, err := buildInstanceSpec(args, "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.Claude.AppendSystemPrompt != "Always respond in Japanese" {
		t.Errorf("AppendSystemPrompt = %q", spec.Claude.AppendSystemPrompt)
	}
	if spec.Claude.FallbackModel != "claude-haiku-4-5-20251001" {
		t.Errorf("FallbackModel = %q", spec.Claude.FallbackModel)
	}
	if spec.Claude.Mode == nil || *spec.Claude.Mode != "chat" {
		t.Errorf("Mode = %v, want %q", spec.Claude.Mode, "chat")
	}
	if len(spec.Claude.AllowedTools) != 3 {
		t.Errorf("AllowedTools = %v, want 3 items", spec.Claude.AllowedTools)
	}
	if len(spec.Claude.DisallowedTools) != 1 || spec.Claude.DisallowedTools[0] != "WebSearch" {
		t.Errorf("DisallowedTools = %v", spec.Claude.DisallowedTools)
	}
	if len(spec.MCPServers) != 2 {
		t.Fatalf("MCPServers = %d, want 2", len(spec.MCPServers))
	}
	if spec.MCPServers[0].Name != "github-server" {
		t.Errorf("MCPServers[0].Name = %q", spec.MCPServers[0].Name)
	}
	if spec.MCPServers[1].Name != "slack-server" {
		t.Errorf("MCPServers[1].Name = %q", spec.MCPServers[1].Name)
	}
}

func TestBuildInstanceSpec_InvalidPermissionMode(t *testing.T) {
	args := map[string]any{
		"permission_mode": "invalid",
	}
	_, err := buildInstanceSpec(args, "user@example.com")
	if err == nil {
		t.Fatal("expected error for invalid permission_mode")
	}
}

func TestBuildInstanceSpec_InvalidEffort(t *testing.T) {
	args := map[string]any{
		"effort": "extreme",
	}
	_, err := buildInstanceSpec(args, "user@example.com")
	if err == nil {
		t.Fatal("expected error for invalid effort")
	}
}

func TestBuildInstanceSpec_InvalidWorkspaceSize(t *testing.T) {
	args := map[string]any{
		"workspace_size": "not-a-quantity",
	}
	_, err := buildInstanceSpec(args, "user@example.com")
	if err == nil {
		t.Fatal("expected error for invalid workspace_size")
	}
}

func TestBuildInstanceSpec_InvalidPlugin(t *testing.T) {
	args := map[string]any{
		"plugins": []any{"no-tag-or-digest"},
	}
	_, err := buildInstanceSpec(args, "user@example.com")
	if err == nil {
		t.Fatal("expected error for plugin without tag/digest")
	}
}

func TestBuildInstanceSpec_WorkspacePartial(t *testing.T) {
	// Providing only workspace_git_repo should still create a WorkspaceConfig.
	args := map[string]any{
		"workspace_git_repo": "https://github.com/org/repo.git",
	}
	spec, err := buildInstanceSpec(args, "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Workspace == nil {
		t.Fatal("Workspace should not be nil when workspace_git_repo is set")
	}
	if spec.Workspace.GitRepo != "https://github.com/org/repo.git" {
		t.Errorf("GitRepo = %q", spec.Workspace.GitRepo)
	}
	if spec.Workspace.GitRef != "" {
		t.Errorf("GitRef = %q, want empty", spec.Workspace.GitRef)
	}
}

func TestBuildInstanceSpec_ModeAgent(t *testing.T) {
	args := map[string]any{
		"mode": "agent",
	}
	spec, err := buildInstanceSpec(args, "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Claude.Mode == nil || *spec.Claude.Mode != "agent" {
		t.Errorf("Mode = %v, want %q", spec.Claude.Mode, "agent")
	}
}

func TestBuildInstanceSpec_InvalidMode(t *testing.T) {
	args := map[string]any{
		"mode": "invalid",
	}
	_, err := buildInstanceSpec(args, "user@example.com")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestParseStringArray(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{name: "nil", v: nil, want: nil},
		{name: "empty slice", v: []any{}, want: []string{}},
		{name: "string items", v: []any{"a", "b"}, want: []string{"a", "b"}},
		{name: "string slice", v: []string{"x", "y"}, want: []string{"x", "y"}},
		{name: "mixed with empty", v: []any{"a", "", "b"}, want: []string{"a", "b"}},
		{name: "non-array", v: "not-an-array", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStringArray(tt.v)
			if tt.want == nil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
