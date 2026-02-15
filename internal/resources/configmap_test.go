package resources

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestBuildConfigMap_SystemPrompt(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				SystemPrompt: "You are a helpful assistant.",
			},
		},
	}
	instance.Name = "test-instance"

	cm, err := BuildConfigMap(instance, "test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cm.Data["system-prompt"] != "You are a helpful assistant." {
		t.Errorf("expected system-prompt in ConfigMap data, got: %q", cm.Data["system-prompt"])
	}
}

func TestBuildConfigMap_MCPConfig(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				MCPServers: map[string]runtime.RawExtension{
					"github": {
						Raw: json.RawMessage(`{"type":"http","url":"https://api.github.com/mcp/"}`),
					},
				},
			},
		},
	}
	instance.Name = "test-instance"

	cm, err := BuildConfigMap(instance, "test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mcpConfig := cm.Data["mcp-config.json"]
	if mcpConfig == "" {
		t.Fatal("expected mcp-config.json in ConfigMap data")
	}
	if !strings.Contains(mcpConfig, `"mcpServers"`) {
		t.Errorf("mcp-config.json should contain mcpServers wrapper, got: %s", mcpConfig)
	}
	if !strings.Contains(mcpConfig, `"github"`) {
		t.Errorf("mcp-config.json should contain github server, got: %s", mcpConfig)
	}
}

func TestBuildConfigMap_Skills(t *testing.T) {
	boolTrue := true
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Skills: map[string]klausv1alpha1.SkillConfig{
				"deploy": {
					Description:   "Deploy using GitOps",
					Content:       "Never apply directly to a cluster.",
					UserInvocable: &boolTrue,
					AllowedTools:  "Read,Grep",
				},
			},
		},
	}
	instance.Name = "test-instance"

	cm, err := BuildConfigMap(instance, "test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	skillContent := cm.Data["skill-deploy"]
	if skillContent == "" {
		t.Fatal("expected skill-deploy in ConfigMap data")
	}
	if !strings.Contains(skillContent, "---") {
		t.Error("skill should have YAML frontmatter delimiters")
	}
	if !strings.Contains(skillContent, `description: "Deploy using GitOps"`) {
		t.Errorf("skill should contain description, got: %s", skillContent)
	}
	if !strings.Contains(skillContent, "userInvocable: true") {
		t.Errorf("skill should contain userInvocable, got: %s", skillContent)
	}
	if !strings.Contains(skillContent, "Never apply directly to a cluster.") {
		t.Errorf("skill should contain body content, got: %s", skillContent)
	}
}

func TestBuildConfigMap_Hooks(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Hooks: map[string]runtime.RawExtension{
				"PreToolUse": {
					Raw: json.RawMessage(`[{"matcher":"Bash","hooks":[{"type":"command","command":"/bin/check"}]}]`),
				},
			},
		},
	}
	instance.Name = "test-instance"

	cm, err := BuildConfigMap(instance, "test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	settingsJSON := cm.Data["settings.json"]
	if settingsJSON == "" {
		t.Fatal("expected settings.json in ConfigMap data")
	}
	if !strings.Contains(settingsJSON, `"hooks"`) {
		t.Errorf("settings.json should contain hooks wrapper, got: %s", settingsJSON)
	}
}

func TestBuildConfigMap_Empty(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
		},
	}
	instance.Name = "test-instance"

	cm, err := BuildConfigMap(instance, "test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cm.Data) != 0 {
		t.Errorf("expected empty ConfigMap data for empty spec, got %d keys", len(cm.Data))
	}
}
