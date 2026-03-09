package mcp

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// parsePluginReference parses an OCI image reference string into a PluginReference.
// Supported formats:
//   - "repository:tag"       (e.g. "gsoci.azurecr.io/giantswarm/plugins/code-reviewer:v0.1.0")
//   - "repository@sha256:…"  (e.g. "gsoci.azurecr.io/giantswarm/plugins/code-reviewer@sha256:abc123")
func parsePluginReference(ref string) (klausv1alpha1.PluginReference, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return klausv1alpha1.PluginReference{}, fmt.Errorf("empty plugin reference")
	}

	// Check for digest first (repo@sha256:...)
	if idx := strings.LastIndex(ref, "@"); idx > 0 {
		return klausv1alpha1.PluginReference{
			Repository: ref[:idx],
			Digest:     ref[idx+1:],
		}, nil
	}

	// OCI refs have format: registry/path:tag
	// The tag portion never contains '/', so the tag colon is the last ':'
	// that appears after the last '/'. When there is no '/', a bare
	// "host:port" string is ambiguous and not a valid plugin reference.
	lastSlash := strings.LastIndex(ref, "/")
	if lastSlash < 0 {
		return klausv1alpha1.PluginReference{}, fmt.Errorf("plugin reference %q must include a repository path with a tag or digest", ref)
	}

	tagPart := ref[lastSlash:]
	colonInTag := strings.LastIndex(tagPart, ":")
	if colonInTag < 0 {
		return klausv1alpha1.PluginReference{}, fmt.Errorf("plugin reference %q must include a tag or digest", ref)
	}

	colonIdx := lastSlash + colonInTag

	return klausv1alpha1.PluginReference{
		Repository: ref[:colonIdx],
		Tag:        ref[colonIdx+1:],
	}, nil
}

// buildInstanceSpec extracts MCP tool arguments and assembles a KlausInstanceSpec.
// The returned spec has Owner, Claude, Personality, Image, Plugins, MCPServers,
// and Workspace fields populated based on the provided arguments. Fields not
// present in args are left at their zero values.
func buildInstanceSpec(args map[string]any, owner string) (klausv1alpha1.KlausInstanceSpec, error) {
	model, _ := args["model"].(string)
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	systemPrompt, _ := args["system_prompt"].(string)
	personality, _ := args["personality"].(string)

	spec := klausv1alpha1.KlausInstanceSpec{
		Owner: owner,
		Claude: klausv1alpha1.ClaudeConfig{
			Model:          model,
			PermissionMode: klausv1alpha1.PermissionModeBypass,
			SystemPrompt:   systemPrompt,
		},
	}

	if personality != "" {
		spec.Personality = personality
	}

	// Image override.
	if v, _ := args["image"].(string); v != "" {
		spec.Image = v
	}

	// Permission mode.
	if v, _ := args["permission_mode"].(string); v != "" {
		switch klausv1alpha1.PermissionMode(v) {
		case klausv1alpha1.PermissionModeBypass, klausv1alpha1.PermissionModeDefault:
			spec.Claude.PermissionMode = klausv1alpha1.PermissionMode(v)
		default:
			return spec, fmt.Errorf("invalid permission_mode %q: must be %q or %q",
				v, klausv1alpha1.PermissionModeBypass, klausv1alpha1.PermissionModeDefault)
		}
	}

	// Max budget USD.
	if v, ok := args["max_budget_usd"].(float64); ok && v > 0 {
		spec.Claude.MaxBudgetUSD = &v
	}

	// Max turns.
	if v, ok := args["max_turns"].(float64); ok && v > 0 {
		turns := int(v)
		spec.Claude.MaxTurns = &turns
	}

	// Effort.
	if v, _ := args["effort"].(string); v != "" {
		switch klausv1alpha1.EffortLevel(v) {
		case klausv1alpha1.EffortLow, klausv1alpha1.EffortMedium, klausv1alpha1.EffortHigh:
			spec.Claude.Effort = klausv1alpha1.EffortLevel(v)
		default:
			return spec, fmt.Errorf("invalid effort %q: must be %q, %q, or %q",
				v, klausv1alpha1.EffortLow, klausv1alpha1.EffortMedium, klausv1alpha1.EffortHigh)
		}
	}

	// Append system prompt.
	if v, _ := args["append_system_prompt"].(string); v != "" {
		spec.Claude.AppendSystemPrompt = v
	}

	// Fallback model.
	if v, _ := args["fallback_model"].(string); v != "" {
		spec.Claude.FallbackModel = v
	}

	// Persistent mode.
	if v, ok := args["persistent_mode"].(bool); ok {
		spec.Claude.PersistentMode = &v
	}

	// Allowed tools.
	if tools := parseStringArray(args["allowed_tools"]); len(tools) > 0 {
		spec.Claude.AllowedTools = tools
	}

	// Disallowed tools.
	if tools := parseStringArray(args["disallowed_tools"]); len(tools) > 0 {
		spec.Claude.DisallowedTools = tools
	}

	// Plugins.
	if pluginRefs := parseStringArray(args["plugins"]); len(pluginRefs) > 0 {
		plugins := make([]klausv1alpha1.PluginReference, 0, len(pluginRefs))
		for _, ref := range pluginRefs {
			p, err := parsePluginReference(ref)
			if err != nil {
				return spec, fmt.Errorf("invalid plugin reference: %w", err)
			}
			plugins = append(plugins, p)
		}
		spec.Plugins = plugins
	}

	// MCP servers (by name).
	if names := parseStringArray(args["mcp_servers"]); len(names) > 0 {
		refs := make([]klausv1alpha1.MCPServerReference, 0, len(names))
		for _, name := range names {
			refs = append(refs, klausv1alpha1.MCPServerReference{Name: name})
		}
		spec.MCPServers = refs
	}

	// Workspace fields -- build a WorkspaceConfig if any workspace param is set.
	var ws klausv1alpha1.WorkspaceConfig
	hasWorkspace := false

	if v, _ := args["workspace_git_repo"].(string); v != "" {
		ws.GitRepo = v
		hasWorkspace = true
	}
	if v, _ := args["workspace_git_ref"].(string); v != "" {
		ws.GitRef = v
		hasWorkspace = true
	}
	if v, _ := args["workspace_git_secret"].(string); v != "" {
		ws.GitSecretRef = &klausv1alpha1.GitSecretReference{Name: v}
		hasWorkspace = true
	}
	if v, _ := args["workspace_storage_class"].(string); v != "" {
		ws.StorageClass = v
		hasWorkspace = true
	}
	if v, _ := args["workspace_size"].(string); v != "" {
		qty, err := resource.ParseQuantity(v)
		if err != nil {
			return spec, fmt.Errorf("invalid workspace_size %q: %w", v, err)
		}
		ws.Size = &qty
		hasWorkspace = true
	}

	if hasWorkspace {
		spec.Workspace = &ws
	}

	return spec, nil
}

// parseStringArray attempts to extract a []string from an MCP argument value.
// MCP-go delivers JSON arrays as []any with string elements.
func parseStringArray(v any) []string {
	if v == nil {
		return nil
	}
	switch arr := v.(type) {
	case []any:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return arr
	default:
		return nil
	}
}
