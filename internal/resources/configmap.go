package resources

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildConfigMap creates the ConfigMap for a KlausInstance, containing all
// configuration data: system prompts, MCP config, skills, agent files, hooks,
// hook scripts, agents JSON, and JSON schema. This mirrors the Helm chart's
// configmap.yaml rendering.
func BuildConfigMap(instance *klausv1alpha1.KlausInstance, namespace string) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	// System prompt.
	if instance.Spec.Claude.SystemPrompt != "" {
		data["system-prompt"] = instance.Spec.Claude.SystemPrompt
	}

	// Append system prompt.
	if instance.Spec.Claude.AppendSystemPrompt != "" {
		data["append-system-prompt"] = instance.Spec.Claude.AppendSystemPrompt
	}

	// MCP config (inline mcpServers rendered as {"mcpServers": {...}}).
	if len(instance.Spec.Claude.MCPServers) > 0 {
		mcpConfig, err := buildMCPConfigJSON(instance.Spec.Claude.MCPServers)
		if err != nil {
			return nil, fmt.Errorf("building MCP config: %w", err)
		}
		data["mcp-config.json"] = mcpConfig
	}

	// JSON schema.
	if instance.Spec.Claude.JSONSchema != "" {
		data["json-schema"] = instance.Spec.Claude.JSONSchema
	}

	// Agents JSON.
	if len(instance.Spec.Claude.Agents) > 0 {
		agentsJSON, err := buildAgentsJSON(instance.Spec.Claude.Agents)
		if err != nil {
			return nil, fmt.Errorf("building agents JSON: %w", err)
		}
		data["agents"] = agentsJSON
	}

	// Skills (SKILL.md with YAML frontmatter).
	skillNames := sortedSkillKeys(instance.Spec.Skills)
	for _, name := range skillNames {
		skill := instance.Spec.Skills[name]
		data["skill-"+name] = renderSkillMD(skill)
	}

	// Agent files (raw markdown).
	agentFileNames := sortedAgentFileKeys(instance.Spec.AgentFiles)
	for _, name := range agentFileNames {
		agentFile := instance.Spec.AgentFiles[name]
		data["agentfile-"+name] = agentFile.Content
	}

	// Hooks (rendered to settings.json).
	if HasHooks(instance) {
		hooksJSON, err := buildHooksJSON(instance.Spec.Hooks)
		if err != nil {
			return nil, fmt.Errorf("building hooks JSON: %w", err)
		}
		data["settings.json"] = hooksJSON
	}

	// Hook scripts.
	scriptNames := sortedStringMapKeys(instance.Spec.HookScripts)
	for _, name := range scriptNames {
		data["hookscript-"+name] = instance.Spec.HookScripts[name]
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(instance),
			Namespace: namespace,
			Labels:    InstanceLabels(instance),
		},
		Data: data,
	}

	return cm, nil
}

func buildMCPConfigJSON(mcpServers map[string]runtime.RawExtension) (string, error) {
	// Convert RawExtension values to json.RawMessage for marshaling.
	servers := make(map[string]json.RawMessage, len(mcpServers))
	for name, raw := range mcpServers {
		servers[name] = json.RawMessage(raw.Raw)
	}

	wrapper := map[string]any{
		"mcpServers": servers,
	}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildAgentsJSON(agents map[string]runtime.RawExtension) (string, error) {
	// Convert RawExtension values to json.RawMessage for marshaling.
	m := make(map[string]json.RawMessage, len(agents))
	for name, raw := range agents {
		m[name] = json.RawMessage(raw.Raw)
	}

	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildHooksJSON(hooks map[string]runtime.RawExtension) (string, error) {
	// Convert RawExtension values to json.RawMessage for marshaling.
	m := make(map[string]json.RawMessage, len(hooks))
	for name, raw := range hooks {
		m[name] = json.RawMessage(raw.Raw)
	}

	wrapper := map[string]any{
		"hooks": m,
	}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// renderSkillMD renders a SKILL.md file with YAML frontmatter, matching the
// Helm chart's rendering pattern.
func renderSkillMD(skill klausv1alpha1.SkillConfig) string {
	var b strings.Builder
	b.WriteString("---\n")

	if skill.Description != "" {
		b.WriteString(fmt.Sprintf("description: %q\n", skill.Description))
	}
	if skill.DisableModelInvocation != nil {
		b.WriteString(fmt.Sprintf("disableModelInvocation: %t\n", *skill.DisableModelInvocation))
	}
	if skill.UserInvocable != nil {
		b.WriteString(fmt.Sprintf("userInvocable: %t\n", *skill.UserInvocable))
	}
	if skill.AllowedTools != "" {
		b.WriteString(fmt.Sprintf("allowedTools: %q\n", skill.AllowedTools))
	}
	if skill.Model != "" {
		b.WriteString(fmt.Sprintf("model: %q\n", skill.Model))
	}
	if skill.Context != nil && skill.Context.Raw != nil {
		b.WriteString(fmt.Sprintf("context:\n  %s\n", string(skill.Context.Raw)))
	}
	if skill.Agent != "" {
		b.WriteString(fmt.Sprintf("agent: %q\n", skill.Agent))
	}
	if skill.ArgumentHint != "" {
		b.WriteString(fmt.Sprintf("argumentHint: %q\n", skill.ArgumentHint))
	}

	b.WriteString("---\n")
	b.WriteString(skill.Content)

	// Ensure trailing newline.
	if !strings.HasSuffix(skill.Content, "\n") {
		b.WriteString("\n")
	}

	return b.String()
}

func sortedSkillKeys(m map[string]klausv1alpha1.SkillConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedAgentFileKeys(m map[string]klausv1alpha1.AgentFileConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
