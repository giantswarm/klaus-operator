package resources

import (
	"maps"
	"path"
	"slices"

	corev1 "k8s.io/api/core/v1"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildVolumes creates the volume list for a KlausInstance pod spec.
func BuildVolumes(instance *klausv1alpha1.KlausInstance, configMapName string) []corev1.Volume {
	var volumes []corev1.Volume

	// Config volume (always present).
	volumes = append(volumes, corev1.Volume{
		Name: ConfigVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
			},
		},
	})

	// Config scripts volume (executable hook scripts, separate volume with mode 0755).
	if NeedsScriptsVolume(instance) {
		execMode := int32(0755)
		volumes = append(volumes, corev1.Volume{
			Name: ConfigScriptsVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
					DefaultMode:          &execMode,
					Items:                buildScriptItems(instance),
				},
			},
		})
	}

	// Plugin volumes (OCI image volumes).
	for _, plugin := range instance.Spec.Plugins {
		volumes = append(volumes, corev1.Volume{
			Name: PluginVolumeName(plugin),
			VolumeSource: corev1.VolumeSource{
				Image: &corev1.ImageVolumeSource{
					Reference:  PluginImageReference(plugin),
					PullPolicy: corev1.PullIfNotPresent,
				},
			},
		})
	}

	// Workspace volume (PVC).
	if instance.Spec.Workspace != nil {
		volumes = append(volumes, corev1.Volume{
			Name: WorkspaceVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: PVCName(instance),
				},
			},
		})
	}

	return volumes
}

// BuildVolumeMounts creates the volume mount list for a KlausInstance container.
func BuildVolumeMounts(instance *klausv1alpha1.KlausInstance) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	// MCP config mount.
	if HasMCPConfig(instance) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      ConfigVolumeName,
			MountPath: MCPConfigPath,
			SubPath:   "mcp-config.json",
			ReadOnly:  true,
		})
	}

	// Skills mounts.
	for _, name := range slices.Sorted(maps.Keys(instance.Spec.Skills)) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      ConfigVolumeName,
			MountPath: path.Join(ExtensionsBasePath, ".claude/skills", name, "SKILL.md"),
			SubPath:   "skill-" + name,
			ReadOnly:  true,
		})
	}

	// Agent file mounts.
	for _, name := range slices.Sorted(maps.Keys(instance.Spec.AgentFiles)) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      ConfigVolumeName,
			MountPath: path.Join(ExtensionsBasePath, ".claude/agents", name+".md"),
			SubPath:   "agentfile-" + name,
			ReadOnly:  true,
		})
	}

	// Settings.json mount (hooks).
	if HasHooks(instance) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      ConfigVolumeName,
			MountPath: SettingsFilePath,
			SubPath:   "settings.json",
			ReadOnly:  true,
		})
	}

	// Hook script mounts (from executable volume).
	if NeedsScriptsVolume(instance) {
		for _, name := range slices.Sorted(maps.Keys(instance.Spec.HookScripts)) {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      ConfigScriptsVolumeName,
				MountPath: path.Join(HookScriptsPath, name),
				SubPath:   "hookscript-" + name,
				ReadOnly:  true,
			})
		}
	}

	// Plugin mounts (OCI image volumes).
	for _, plugin := range instance.Spec.Plugins {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      PluginVolumeName(plugin),
			MountPath: PluginMountPath(plugin),
			ReadOnly:  true,
		})
	}

	// Workspace mount.
	if instance.Spec.Workspace != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      WorkspaceVolumeName,
			MountPath: WorkspaceMountPath,
		})
	}

	return mounts
}

func buildScriptItems(instance *klausv1alpha1.KlausInstance) []corev1.KeyToPath {
	var items []corev1.KeyToPath
	for _, name := range slices.Sorted(maps.Keys(instance.Spec.HookScripts)) {
		items = append(items, corev1.KeyToPath{
			Key:  "hookscript-" + name,
			Path: "hookscript-" + name,
			// Mode is omitted; DefaultMode (0755) on the volume applies.
		})
	}
	return items
}
