// Package resources provides functions to build Kubernetes resources
// for a KlausInstance, mirroring the patterns from the standalone Helm chart.
package resources

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

const (
	// KlausPort is the default port for the Klaus agent HTTP server.
	KlausPort = 8080

	// ConfigVolumeName is the name of the ConfigMap volume.
	ConfigVolumeName = "config"

	// ConfigScriptsVolumeName is the name of the executable hook scripts volume.
	ConfigScriptsVolumeName = "config-scripts"

	// WorkspaceVolumeName is the name of the workspace PVC volume.
	WorkspaceVolumeName = "workspace"

	// WorkspaceMountPath is where the workspace PVC is mounted.
	WorkspaceMountPath = "/workspace"

	// MCPConfigPath is the path to the MCP config file inside the container.
	MCPConfigPath = "/etc/klaus/mcp-config.json"

	// SettingsFilePath is the path to settings.json inside the container.
	SettingsFilePath = "/etc/klaus/settings.json"

	// ExtensionsBasePath is the base path for inline skills and agent files.
	ExtensionsBasePath = "/etc/klaus/extensions"

	// PluginBasePath is the base path for OCI plugin mounts.
	PluginBasePath = "/var/lib/klaus/plugins"

	// HookScriptsPath is the base path for hook scripts.
	HookScriptsPath = "/etc/klaus/hooks"
)

var sanitizeRegexp = regexp.MustCompile(`[^a-z0-9-]`)

// UserNamespace returns the namespace name for a given owner.
func UserNamespace(owner string) string {
	sanitized := strings.ToLower(owner)
	// Replace @ and . with hyphens.
	sanitized = strings.ReplaceAll(sanitized, "@", "-")
	sanitized = strings.ReplaceAll(sanitized, ".", "-")
	sanitized = sanitizeRegexp.ReplaceAllString(sanitized, "-")
	// Trim leading/trailing hyphens.
	sanitized = strings.Trim(sanitized, "-")
	// Truncate if necessary (namespace max is 63 chars, prefix is 11).
	if len(sanitized) > 50 {
		sanitized = sanitized[:50]
	}
	return "klaus-user-" + sanitized
}

// InstanceLabels returns standard labels for resources owned by the instance.
func InstanceLabels(instance *klausv1alpha1.KlausInstance) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "klaus",
		"app.kubernetes.io/instance":   instance.Name,
		"app.kubernetes.io/managed-by": "klaus-operator",
		"klaus.giantswarm.io/owner":    sanitizeLabelValue(instance.Spec.Owner),
	}
}

// ConfigMapName returns the ConfigMap name for an instance.
func ConfigMapName(instance *klausv1alpha1.KlausInstance) string {
	return instance.Name + "-config"
}

// ServiceName returns the Service name for an instance.
func ServiceName(instance *klausv1alpha1.KlausInstance) string {
	return instance.Name
}

// PVCName returns the PVC name for an instance.
func PVCName(instance *klausv1alpha1.KlausInstance) string {
	return instance.Name + "-workspace"
}

// SecretName returns the copied API key Secret name for an instance.
func SecretName(instance *klausv1alpha1.KlausInstance) string {
	return instance.Name + "-api-key"
}

// ShortPluginName extracts the last path segment from an OCI repository.
func ShortPluginName(repository string) string {
	parts := strings.Split(repository, "/")
	return parts[len(parts)-1]
}

// PluginVolumeName returns the volume name for a plugin.
func PluginVolumeName(plugin klausv1alpha1.PluginReference) string {
	return "plugin-" + ShortPluginName(plugin.Repository)
}

// PluginImageReference returns the full image reference for a plugin.
func PluginImageReference(plugin klausv1alpha1.PluginReference) string {
	if plugin.Digest != "" {
		return plugin.Repository + "@" + plugin.Digest
	}
	return plugin.Repository + ":" + plugin.Tag
}

// PluginMountPath returns the mount path for a plugin.
func PluginMountPath(plugin klausv1alpha1.PluginReference) string {
	return path.Join(PluginBasePath, ShortPluginName(plugin.Repository))
}

// ConfigMapChecksum computes a SHA256 checksum of the ConfigMap data for
// triggering pod restarts on config changes.
func ConfigMapChecksum(data map[string]string) string {
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, data[k])
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// HasInlineExtensions returns true if the instance has skills or agent files
// that need the extensions directory in CLAUDE_ADD_DIRS.
func HasInlineExtensions(instance *klausv1alpha1.KlausInstance) bool {
	return len(instance.Spec.Skills) > 0 || len(instance.Spec.AgentFiles) > 0
}

// NeedsScriptsVolume returns true if hook scripts need a separate executable volume.
func NeedsScriptsVolume(instance *klausv1alpha1.KlausInstance) bool {
	return len(instance.Spec.HookScripts) > 0
}

// HasMCPConfig returns true if any MCP servers are configured.
func HasMCPConfig(instance *klausv1alpha1.KlausInstance) bool {
	return len(instance.Spec.Claude.MCPServers) > 0 || len(instance.Spec.MCPServers) > 0
}

// HasHooks returns true if hooks are configured.
func HasHooks(instance *klausv1alpha1.KlausInstance) bool {
	return len(instance.Spec.Hooks) > 0
}

// MarshalJSONMap serializes a map of RawExtensions to a JSON string.
func MarshalJSONMap(m map[string]json.RawMessage) (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshaling JSON map: %w", err)
	}
	return string(data), nil
}

func sanitizeLabelValue(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "@", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = sanitizeRegexp.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}
