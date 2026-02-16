package resources

import (
	"encoding/json"
	"maps"
	"slices"

	"k8s.io/apimachinery/pkg/runtime"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// ResolvedMCPConfig holds the aggregated MCP server configurations and secrets
// resolved from KlausMCPServer CRD references.
type ResolvedMCPConfig struct {
	// Servers maps KlausMCPServer name to its JSON config for .mcp.json assembly.
	Servers map[string]runtime.RawExtension

	// Secrets holds aggregated secretRefs from all resolved KlausMCPServer objects.
	Secrets []klausv1alpha1.MCPServerSecret
}

// ServerConfigToRawExtension converts a KlausMCPServerSpec into a
// runtime.RawExtension containing the MCP server config JSON. The secretRefs
// field is excluded -- it is used for pod-level env injection only, not for
// .mcp.json assembly.
func ServerConfigToRawExtension(spec *klausv1alpha1.KlausMCPServerSpec) (runtime.RawExtension, error) {
	config := make(map[string]any)

	if spec.Type != "" {
		config["type"] = spec.Type
	}
	if spec.URL != "" {
		config["url"] = spec.URL
	}
	if spec.Command != "" {
		config["command"] = spec.Command
	}
	if len(spec.Args) > 0 {
		config["args"] = spec.Args
	}
	if len(spec.Env) > 0 {
		config["env"] = spec.Env
	}
	if len(spec.Headers) > 0 {
		config["headers"] = spec.Headers
	}

	raw, err := json.Marshal(config)
	if err != nil {
		return runtime.RawExtension{}, err
	}
	return runtime.RawExtension{Raw: raw}, nil
}

// MergeResolvedMCPIntoInstance injects resolved MCP server configurations and
// secrets into the instance spec. This should be called on a deep-copied
// instance after personality merge and MCP server resolution.
//
// Merge semantics:
//   - Server configs: resolved entries are added to claude.mcpServers; on key
//     conflict, the resolved KlausMCPServer entry wins over inline config.
//   - Secrets: resolved secretRefs are appended and deduplicated by env var
//     name (resolved wins over inline on conflict).
func MergeResolvedMCPIntoInstance(resolved *ResolvedMCPConfig, instance *klausv1alpha1.KlausInstanceSpec) {
	if resolved == nil {
		return
	}

	// Merge server configs into claude.mcpServers.
	if len(resolved.Servers) > 0 {
		if instance.Claude.MCPServers == nil {
			instance.Claude.MCPServers = make(map[string]runtime.RawExtension, len(resolved.Servers))
		}
		for name, config := range resolved.Servers {
			instance.Claude.MCPServers[name] = config
		}
	}

	// Merge and deduplicate secrets.
	if len(resolved.Secrets) > 0 {
		instance.Claude.MCPServerSecrets = DeduplicateMCPServerSecrets(
			instance.Claude.MCPServerSecrets,
			resolved.Secrets,
		)
	}
}

// DeduplicateMCPServerSecrets merges inline and resolved MCP server secrets,
// deduplicating by environment variable name. When both inline and resolved
// define the same env var, the resolved entry wins.
//
// The returned list is grouped by Secret name with deterministic ordering.
func DeduplicateMCPServerSecrets(inline, resolved []klausv1alpha1.MCPServerSecret) []klausv1alpha1.MCPServerSecret {
	if len(inline) == 0 && len(resolved) == 0 {
		return nil
	}

	type secretKeyRef struct {
		secretName string
		key        string
	}

	// Build a map of env var name -> source. Inline first, then resolved
	// overwrites on conflict.
	envMap := make(map[string]secretKeyRef)
	for _, s := range inline {
		for envVar, key := range s.Env {
			envMap[envVar] = secretKeyRef{secretName: s.SecretName, key: key}
		}
	}
	for _, s := range resolved {
		for envVar, key := range s.Env {
			envMap[envVar] = secretKeyRef{secretName: s.SecretName, key: key}
		}
	}

	// Group back by Secret name for a compact representation.
	secretEnvs := make(map[string]map[string]string)
	for envVar, ref := range envMap {
		if secretEnvs[ref.secretName] == nil {
			secretEnvs[ref.secretName] = make(map[string]string)
		}
		secretEnvs[ref.secretName][envVar] = ref.key
	}

	// Convert to a sorted slice for deterministic output.
	result := make([]klausv1alpha1.MCPServerSecret, 0, len(secretEnvs))
	for _, secretName := range slices.Sorted(maps.Keys(secretEnvs)) {
		result = append(result, klausv1alpha1.MCPServerSecret{
			SecretName: secretName,
			Env:        secretEnvs[secretName],
		})
	}
	return result
}
