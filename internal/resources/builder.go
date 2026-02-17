// Package resources provides functions to build Kubernetes resources
// for a KlausInstance, mirroring the patterns from the standalone Helm chart.
package resources

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
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

	// GitSecretVolumeName is the name of the git secret volume.
	GitSecretVolumeName = "git-secret"

	// GitSecretMountPath is where the git secret is mounted in the init container.
	GitSecretMountPath = "/etc/git-secret"

	// DefaultGitSecretKey is the default key in the git Secret data.
	DefaultGitSecretKey = "token"

	// DefaultGitCloneImage is the default image for the git clone init container.
	// Pinned to a specific version for reproducible deployments; override via
	// the --git-clone-image flag.
	DefaultGitCloneImage = "alpine/git:v2.47.2"
)

var sanitizeRegexp = regexp.MustCompile(`[^a-z0-9-]`)

// UserNamespace returns the namespace name for a given owner.
func UserNamespace(owner string) string {
	// Prefix is 11 chars; namespace max is 63.
	return "klaus-user-" + sanitizeIdentifier(owner, 50)
}

// SelectorLabels returns the minimal label set used for pod selection by both
// the Deployment and Service. Keeping these in sync is critical -- if they
// diverge, the Service silently stops matching pods.
func SelectorLabels(instance *klausv1alpha1.KlausInstance) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "klaus",
		"app.kubernetes.io/instance": instance.Name,
	}
}

// InstanceLabels returns standard labels for resources owned by the instance.
func InstanceLabels(instance *klausv1alpha1.KlausInstance) map[string]string {
	labels := SelectorLabels(instance)
	labels["app.kubernetes.io/managed-by"] = "klaus-operator"
	labels["klaus.giantswarm.io/owner"] = sanitizeLabelValue(instance.Spec.Owner)
	return labels
}

// MCPSecretLabels returns labels for MCP secrets copied to user namespaces.
// These secrets may be shared by multiple instances for the same owner, so we
// use managed-by and owner labels without instance-specific identifiers.
func MCPSecretLabels(owner string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "klaus",
		"app.kubernetes.io/managed-by": "klaus-operator",
		"app.kubernetes.io/component":  "mcp-secret",
		"klaus.giantswarm.io/owner":    sanitizeLabelValue(owner),
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

// GitSecretName returns the copied git credential Secret name for an instance.
func GitSecretName(instance *klausv1alpha1.KlausInstance) string {
	return instance.Name + "-git-creds"
}

// GitSecretKey returns the Secret data key for the git credential, defaulting
// to "ssh-privatekey" when unset.
func GitSecretKey(instance *klausv1alpha1.KlausInstance) string {
	if instance.Spec.Workspace != nil &&
		instance.Spec.Workspace.GitSecretRef != nil &&
		instance.Spec.Workspace.GitSecretRef.Key != "" {
		return instance.Spec.Workspace.GitSecretRef.Key
	}
	return DefaultGitSecretKey
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
	h := sha256.New()
	for _, k := range slices.Sorted(maps.Keys(data)) {
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

// MusterNamespace returns the target namespace for MCPServer CRD registration.
func MusterNamespace(instance *klausv1alpha1.KlausInstance) string {
	if instance.Spec.Muster != nil && instance.Spec.Muster.Namespace != "" {
		return instance.Spec.Muster.Namespace
	}
	return "muster"
}

// HasMCPConfig returns true if any MCP servers are configured.
func HasMCPConfig(instance *klausv1alpha1.KlausInstance) bool {
	return len(instance.Spec.Claude.MCPServers) > 0 || len(instance.Spec.MCPServers) > 0
}

// HasHooks returns true if hooks are configured.
func HasHooks(instance *klausv1alpha1.KlausInstance) bool {
	return len(instance.Spec.Hooks) > 0
}

// NeedsGitClone returns true if the workspace has a git repo to clone.
func NeedsGitClone(instance *klausv1alpha1.KlausInstance) bool {
	return instance.Spec.Workspace != nil && instance.Spec.Workspace.GitRepo != ""
}

// NeedsGitSecret returns true if a git secret reference is configured for the workspace.
func NeedsGitSecret(instance *klausv1alpha1.KlausInstance) bool {
	return instance.Spec.Workspace != nil && instance.Spec.Workspace.GitSecretRef != nil
}

// shellQuote wraps a value in POSIX single quotes for safe shell
// interpolation. Single quotes inside the value are properly escaped.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func sanitizeLabelValue(s string) string {
	return sanitizeIdentifier(s, 63)
}

// sanitizeIdentifier converts a string to a DNS-safe identifier by lowercasing,
// replacing non-alphanumeric characters with hyphens, and trimming/truncating.
func sanitizeIdentifier(s string, maxLen int) string {
	s = strings.ToLower(s)
	s = sanitizeRegexp.ReplaceAllString(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	// Trim after truncation to avoid trailing hyphens from the cut.
	s = strings.Trim(s, "-")
	return s
}
