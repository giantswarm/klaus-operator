package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildConfigMap creates the ConfigMap for a KlausInstance, containing all
// configuration data: system prompts, MCP config, skills, agent files, hooks,
// hook scripts, agents JSON, and JSON schema.
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
		mcpConfig, err := marshalRawExtensionMap(instance.Spec.Claude.MCPServers, "mcpServers")
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
		agentsJSON, err := marshalRawExtensionMap(instance.Spec.Claude.Agents, "")
		if err != nil {
			return nil, fmt.Errorf("building agents JSON: %w", err)
		}
		data["agents"] = agentsJSON
	}

	// Skills (SKILL.md with YAML frontmatter).
	for _, name := range slices.Sorted(maps.Keys(instance.Spec.Skills)) {
		skill := instance.Spec.Skills[name]
		data["skill-"+name] = renderSkillMD(skill)
	}

	// Agent files (raw markdown).
	for _, name := range slices.Sorted(maps.Keys(instance.Spec.AgentFiles)) {
		agentFile := instance.Spec.AgentFiles[name]
		data["agentfile-"+name] = agentFile.Content
	}

	// Hooks (rendered to settings.json).
	if HasHooks(instance) {
		hooksJSON, err := marshalRawExtensionMap(instance.Spec.Hooks, "hooks")
		if err != nil {
			return nil, fmt.Errorf("building hooks JSON: %w", err)
		}
		data["settings.json"] = hooksJSON
	}

	// Hook scripts.
	for _, name := range slices.Sorted(maps.Keys(instance.Spec.HookScripts)) {
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

// marshalRawExtensionMap converts a map of RawExtensions to a JSON string.
// If wrapperKey is non-empty, the map is wrapped under that key; otherwise
// it is serialized directly.
func marshalRawExtensionMap(m map[string]runtime.RawExtension, wrapperKey string) (string, error) {
	raw := make(map[string]json.RawMessage, len(m))
	for name, ext := range m {
		raw[name] = json.RawMessage(ext.Raw)
	}

	var target any = raw
	if wrapperKey != "" {
		target = map[string]any{wrapperKey: raw}
	}

	data, err := json.MarshalIndent(target, "", "  ")
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
	if len(skill.AllowedTools) > 0 {
		b.WriteString(fmt.Sprintf("allowedTools: %q\n", strings.Join(skill.AllowedTools, ",")))
	}
	if skill.Model != "" {
		b.WriteString(fmt.Sprintf("model: %q\n", skill.Model))
	}
	if skill.Context != nil && skill.Context.Raw != nil {
		// Compact the JSON to a single line to avoid breaking YAML frontmatter
		// structure with multi-line or YAML-special content.
		compacted, err := compactJSON(skill.Context.Raw)
		if err == nil {
			b.WriteString(fmt.Sprintf("context:\n  %s\n", compacted))
		} else {
			// Fall back to raw content if compaction fails.
			b.WriteString(fmt.Sprintf("context:\n  %s\n", string(skill.Context.Raw)))
		}
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

// compactJSON marshals raw JSON bytes into a single-line representation.
func compactJSON(raw []byte) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return "", err
	}
	return buf.String(), nil
}
