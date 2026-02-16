package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestMergePersonalityIntoInstance_ScalarFieldsInstanceOverrides(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		Claude: klausv1alpha1.ClaudeConfig{
			Model:              "claude-sonnet-4-20250514",
			SystemPrompt:       "You are a platform engineer",
			Effort:             klausv1alpha1.EffortMedium,
			FallbackModel:      "claude-haiku",
			PermissionMode:     klausv1alpha1.PermissionModeBypass,
			AppendSystemPrompt: "Be concise",
			ActiveAgent:        "default-agent",
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			Model: "claude-opus-4-20250514",
			// Other fields are empty/zero -- should inherit from personality.
		},
	}

	MergePersonalityIntoInstance(personality, instance)

	if instance.Claude.Model != "claude-opus-4-20250514" {
		t.Errorf("expected model to be instance override 'claude-opus-4-20250514', got %q", instance.Claude.Model)
	}
	if instance.Claude.SystemPrompt != "You are a platform engineer" {
		t.Errorf("expected system prompt from personality, got %q", instance.Claude.SystemPrompt)
	}
	if instance.Claude.Effort != klausv1alpha1.EffortMedium {
		t.Errorf("expected effort from personality, got %q", instance.Claude.Effort)
	}
	if instance.Claude.FallbackModel != "claude-haiku" {
		t.Errorf("expected fallback model from personality, got %q", instance.Claude.FallbackModel)
	}
	if instance.Claude.PermissionMode != klausv1alpha1.PermissionModeBypass {
		t.Errorf("expected permission mode from personality, got %q", instance.Claude.PermissionMode)
	}
	if instance.Claude.AppendSystemPrompt != "Be concise" {
		t.Errorf("expected append system prompt from personality, got %q", instance.Claude.AppendSystemPrompt)
	}
	if instance.Claude.ActiveAgent != "default-agent" {
		t.Errorf("expected active agent from personality, got %q", instance.Claude.ActiveAgent)
	}
}

func TestMergePersonalityIntoInstance_ScalarFieldsEmptyInstanceInherits(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		Claude: klausv1alpha1.ClaudeConfig{
			Model:          "personality-model",
			MaxTurns:       ptr.To(10),
			MaxBudgetUSD:   ptr.To(5.0),
			MCPTimeout:     ptr.To(30000),
			JSONSchema:     `{"type":"object"}`,
			SettingsFile:   "/custom/settings.json",
			SettingSources: "project,local",
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{}

	MergePersonalityIntoInstance(personality, instance)

	if instance.Claude.Model != "personality-model" {
		t.Errorf("expected model from personality, got %q", instance.Claude.Model)
	}
	if instance.Claude.MaxTurns == nil || *instance.Claude.MaxTurns != 10 {
		t.Errorf("expected maxTurns 10 from personality")
	}
	if instance.Claude.MaxBudgetUSD == nil || *instance.Claude.MaxBudgetUSD != 5.0 {
		t.Errorf("expected maxBudgetUSD 5.0 from personality")
	}
	if instance.Claude.MCPTimeout == nil || *instance.Claude.MCPTimeout != 30000 {
		t.Errorf("expected mcpTimeout 30000 from personality")
	}
	if instance.Claude.JSONSchema != `{"type":"object"}` {
		t.Errorf("expected jsonSchema from personality")
	}
}

func TestMergePersonalityIntoInstance_BoolPointersInstanceOverrides(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		Claude: klausv1alpha1.ClaudeConfig{
			PersistentMode:       ptr.To(true),
			NoSessionPersistence: ptr.To(true),
			StrictMCPConfig:      ptr.To(true),
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			PersistentMode: ptr.To(false), // Explicit override.
			// NoSessionPersistence is nil -- should inherit from personality.
		},
	}

	MergePersonalityIntoInstance(personality, instance)

	if *instance.Claude.PersistentMode != false {
		t.Error("expected persistentMode to be overridden to false by instance")
	}
	if instance.Claude.NoSessionPersistence == nil || *instance.Claude.NoSessionPersistence != true {
		t.Error("expected noSessionPersistence to inherit true from personality")
	}
	if instance.Claude.StrictMCPConfig == nil || *instance.Claude.StrictMCPConfig != true {
		t.Error("expected strictMcpConfig to inherit true from personality")
	}
}

func TestMergePersonalityIntoInstance_PluginsMergedAndDeduplicated(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		Plugins: []klausv1alpha1.PluginReference{
			{Repository: "gsoci.azurecr.io/giantswarm/gs-platform", Tag: "v1.0.0"},
			{Repository: "gsoci.azurecr.io/giantswarm/security", Tag: "v0.3.0"},
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Plugins: []klausv1alpha1.PluginReference{
			// Override the security plugin version.
			{Repository: "gsoci.azurecr.io/giantswarm/security", Tag: "v0.4.0"},
			// Add a new plugin.
			{Repository: "gsoci.azurecr.io/giantswarm/team-tools", Tag: "v1.0.0"},
		},
	}

	MergePersonalityIntoInstance(personality, instance)

	if len(instance.Plugins) != 3 {
		t.Fatalf("expected 3 plugins after merge, got %d", len(instance.Plugins))
	}

	// First: gs-platform from personality.
	if instance.Plugins[0].Repository != "gsoci.azurecr.io/giantswarm/gs-platform" || instance.Plugins[0].Tag != "v1.0.0" {
		t.Errorf("plugin 0 should be gs-platform:v1.0.0, got %s:%s", instance.Plugins[0].Repository, instance.Plugins[0].Tag)
	}
	// Second: security overridden by instance.
	if instance.Plugins[1].Repository != "gsoci.azurecr.io/giantswarm/security" || instance.Plugins[1].Tag != "v0.4.0" {
		t.Errorf("plugin 1 should be security:v0.4.0 (instance override), got %s:%s", instance.Plugins[1].Repository, instance.Plugins[1].Tag)
	}
	// Third: team-tools from instance.
	if instance.Plugins[2].Repository != "gsoci.azurecr.io/giantswarm/team-tools" || instance.Plugins[2].Tag != "v1.0.0" {
		t.Errorf("plugin 2 should be team-tools:v1.0.0, got %s:%s", instance.Plugins[2].Repository, instance.Plugins[2].Tag)
	}
}

func TestMergePersonalityIntoInstance_ListFieldsAppended(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		PluginDirs: []string{"/personality/plugins"},
		AddDirs:    []string{"/personality/extensions"},
		Claude: klausv1alpha1.ClaudeConfig{
			Tools:           []string{"Read", "Write"},
			AllowedTools:    []string{"Bash"},
			DisallowedTools: []string{"Delete"},
		},
		MCPServers: []klausv1alpha1.MCPServerReference{
			{Name: "github-mcp"},
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		PluginDirs: []string{"/instance/plugins"},
		AddDirs:    []string{"/instance/extensions"},
		Claude: klausv1alpha1.ClaudeConfig{
			Tools:        []string{"Grep"},
			AllowedTools: []string{"Shell"},
		},
		MCPServers: []klausv1alpha1.MCPServerReference{
			{Name: "sentry-mcp"},
			{Name: "github-mcp"}, // Duplicate -- should be deduped.
		},
	}

	MergePersonalityIntoInstance(personality, instance)

	// PluginDirs: personality + instance.
	if len(instance.PluginDirs) != 2 {
		t.Fatalf("expected 2 plugin dirs, got %d", len(instance.PluginDirs))
	}
	if instance.PluginDirs[0] != "/personality/plugins" || instance.PluginDirs[1] != "/instance/plugins" {
		t.Errorf("unexpected plugin dirs: %v", instance.PluginDirs)
	}

	// AddDirs: personality + instance.
	if len(instance.AddDirs) != 2 {
		t.Fatalf("expected 2 add dirs, got %d", len(instance.AddDirs))
	}

	// Tools: personality + instance.
	if len(instance.Claude.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(instance.Claude.Tools))
	}

	// AllowedTools: personality + instance.
	if len(instance.Claude.AllowedTools) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d", len(instance.Claude.AllowedTools))
	}

	// DisallowedTools: personality only (instance had none).
	if len(instance.Claude.DisallowedTools) != 1 {
		t.Fatalf("expected 1 disallowed tools, got %d", len(instance.Claude.DisallowedTools))
	}

	// MCPServers: deduplicated.
	if len(instance.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers (deduped), got %d", len(instance.MCPServers))
	}
	if instance.MCPServers[0].Name != "github-mcp" || instance.MCPServers[1].Name != "sentry-mcp" {
		t.Errorf("unexpected MCP servers: %v", instance.MCPServers)
	}
}

func TestMergePersonalityIntoInstance_MapFieldsInstanceWins(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		Skills: map[string]klausv1alpha1.SkillConfig{
			"deploy":   {Content: "personality deploy skill"},
			"security": {Content: "personality security skill"},
		},
		AgentFiles: map[string]klausv1alpha1.AgentFileConfig{
			"researcher": {Content: "personality researcher agent"},
		},
		HookScripts: map[string]string{
			"pre-check":        "#!/bin/bash\nexit 0",
			"personality-hook": "#!/bin/bash\necho personality",
		},
		Claude: klausv1alpha1.ClaudeConfig{
			MCPServers: map[string]runtime.RawExtension{
				"shared-server": {Raw: []byte(`{"command":"shared"}`)},
			},
			Agents: map[string]runtime.RawExtension{
				"default": {Raw: []byte(`{"prompt":"default agent"}`)},
			},
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Skills: map[string]klausv1alpha1.SkillConfig{
			"deploy": {Content: "instance deploy skill override"},
		},
		AgentFiles: map[string]klausv1alpha1.AgentFileConfig{
			"researcher": {Content: "instance researcher override"},
			"debugger":   {Content: "instance-only debugger agent"},
		},
		HookScripts: map[string]string{
			"pre-check": "#!/bin/bash\nexit 1",
		},
		Claude: klausv1alpha1.ClaudeConfig{
			MCPServers: map[string]runtime.RawExtension{
				"instance-server": {Raw: []byte(`{"command":"instance"}`)},
			},
		},
	}

	MergePersonalityIntoInstance(personality, instance)

	// Skills: 2 from personality + 1 override = 2 unique keys.
	if len(instance.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(instance.Skills))
	}
	if instance.Skills["deploy"].Content != "instance deploy skill override" {
		t.Error("expected deploy skill to be instance override")
	}
	if instance.Skills["security"].Content != "personality security skill" {
		t.Error("expected security skill from personality")
	}

	// AgentFiles: personality (1) + instance (2 with 1 override) = 2 unique.
	if len(instance.AgentFiles) != 2 {
		t.Fatalf("expected 2 agent files, got %d", len(instance.AgentFiles))
	}
	if instance.AgentFiles["researcher"].Content != "instance researcher override" {
		t.Error("expected researcher to be instance override")
	}
	if instance.AgentFiles["debugger"].Content != "instance-only debugger agent" {
		t.Error("expected debugger from instance")
	}

	// HookScripts: merged, instance wins on conflict.
	if len(instance.HookScripts) != 2 {
		t.Fatalf("expected 2 hook scripts, got %d", len(instance.HookScripts))
	}
	if instance.HookScripts["pre-check"] != "#!/bin/bash\nexit 1" {
		t.Error("expected pre-check to be instance override")
	}
	if instance.HookScripts["personality-hook"] != "#!/bin/bash\necho personality" {
		t.Error("expected personality-hook from personality")
	}

	// MCPServers: merged maps, both present.
	if len(instance.Claude.MCPServers) != 2 {
		t.Fatalf("expected 2 inline MCP servers, got %d", len(instance.Claude.MCPServers))
	}

	// Agents: personality only (instance had none).
	if len(instance.Claude.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(instance.Claude.Agents))
	}
}

func TestMergePersonalityIntoInstance_PointerFieldsInheritedWhenNil(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		LoadAdditionalDirsMemory: ptr.To(true),
		Telemetry: &klausv1alpha1.TelemetryConfig{
			Enabled:         ptr.To(true),
			MetricsExporter: "otlp",
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		// All pointer fields nil -- should inherit from personality.
	}

	MergePersonalityIntoInstance(personality, instance)

	if instance.LoadAdditionalDirsMemory == nil || !*instance.LoadAdditionalDirsMemory {
		t.Error("expected loadAdditionalDirsMemory inherited from personality")
	}
	if instance.Telemetry == nil || instance.Telemetry.MetricsExporter != "otlp" {
		t.Error("expected telemetry inherited from personality")
	}
}

func TestMergePersonalityIntoInstance_EmptyPersonality(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			Model: "instance-model",
		},
		Plugins: []klausv1alpha1.PluginReference{
			{Repository: "repo/plugin", Tag: "v1"},
		},
	}

	MergePersonalityIntoInstance(personality, instance)

	if instance.Claude.Model != "instance-model" {
		t.Error("empty personality should not change instance model")
	}
	if len(instance.Plugins) != 1 {
		t.Error("empty personality should not change instance plugins")
	}
}

func TestMergePersonalityIntoInstance_EmptyInstance(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		Claude: klausv1alpha1.ClaudeConfig{
			Model:          "personality-model",
			PermissionMode: klausv1alpha1.PermissionModeBypass,
		},
		Plugins: []klausv1alpha1.PluginReference{
			{Repository: "repo/plugin", Tag: "v1"},
		},
		Skills: map[string]klausv1alpha1.SkillConfig{
			"test": {Content: "test skill"},
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{}

	MergePersonalityIntoInstance(personality, instance)

	if instance.Claude.Model != "personality-model" {
		t.Errorf("expected model from personality, got %q", instance.Claude.Model)
	}
	if instance.Claude.PermissionMode != klausv1alpha1.PermissionModeBypass {
		t.Errorf("expected permission mode from personality")
	}
	if len(instance.Plugins) != 1 {
		t.Errorf("expected 1 plugin from personality, got %d", len(instance.Plugins))
	}
	if len(instance.Skills) != 1 {
		t.Errorf("expected 1 skill from personality, got %d", len(instance.Skills))
	}
}

func TestMergePersonalityIntoInstance_MCPServerSecretsDeduplicated(t *testing.T) {
	personality := &klausv1alpha1.KlausPersonalitySpec{
		Claude: klausv1alpha1.ClaudeConfig{
			MCPServerSecrets: []klausv1alpha1.MCPServerSecret{
				{SecretName: "shared-secret", Env: map[string]string{"TOKEN": "personality-key"}},
			},
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			MCPServerSecrets: []klausv1alpha1.MCPServerSecret{
				{SecretName: "shared-secret", Env: map[string]string{"TOKEN": "instance-key"}}, // Override.
				{SecretName: "instance-secret", Env: map[string]string{"API_KEY": "key"}},
			},
		},
	}

	MergePersonalityIntoInstance(personality, instance)

	if len(instance.Claude.MCPServerSecrets) != 2 {
		t.Fatalf("expected 2 MCP server secrets (deduped), got %d", len(instance.Claude.MCPServerSecrets))
	}
	// shared-secret appears first (personality position) but with instance's env (instance overrides).
	if instance.Claude.MCPServerSecrets[0].SecretName != "shared-secret" {
		t.Errorf("expected first secret to be shared-secret")
	}
	if instance.Claude.MCPServerSecrets[0].Env["TOKEN"] != "instance-key" {
		t.Errorf("expected shared-secret TOKEN to be instance override 'instance-key', got %q",
			instance.Claude.MCPServerSecrets[0].Env["TOKEN"])
	}
	if instance.Claude.MCPServerSecrets[1].SecretName != "instance-secret" {
		t.Errorf("expected second secret to be instance-secret")
	}
}

func TestMergePlugins_EmptyInputs(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		result := mergePlugins(nil, nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("personality only", func(t *testing.T) {
		plugins := []klausv1alpha1.PluginReference{{Repository: "a", Tag: "v1"}}
		result := mergePlugins(plugins, nil)
		if len(result) != 1 || result[0].Repository != "a" {
			t.Errorf("expected personality plugins, got %v", result)
		}
	})

	t.Run("instance only", func(t *testing.T) {
		plugins := []klausv1alpha1.PluginReference{{Repository: "b", Tag: "v2"}}
		result := mergePlugins(nil, plugins)
		if len(result) != 1 || result[0].Repository != "b" {
			t.Errorf("expected instance plugins, got %v", result)
		}
	})
}
