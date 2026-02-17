package resources

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestBuildDeployment_Basic(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				Model:          "claude-sonnet-4-20250514",
				PermissionMode: klausv1alpha1.PermissionModeBypass,
			},
		},
	}

	configData := map[string]string{"system-prompt": "test prompt"}
	dep := BuildDeployment(instance, "klaus-user-test", "gsoci.azurecr.io/giantswarm/klaus:v1.0.0", DefaultGitCloneImage, configData)

	if dep.Name != "test-instance" {
		t.Errorf("Name = %q, want %q", dep.Name, "test-instance")
	}
	if dep.Namespace != "klaus-user-test" {
		t.Errorf("Namespace = %q, want %q", dep.Namespace, "klaus-user-test")
	}
	if *dep.Spec.Replicas != 1 {
		t.Errorf("Replicas = %d, want 1", *dep.Spec.Replicas)
	}

	// Verify selector labels match pod template labels.
	selectorLabels := dep.Spec.Selector.MatchLabels
	podLabels := dep.Spec.Template.Labels
	for k, v := range selectorLabels {
		if podLabels[k] != v {
			t.Errorf("selector label %q=%q not found in pod template labels", k, v)
		}
	}

	// Verify container image.
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	if containers[0].Image != "gsoci.azurecr.io/giantswarm/klaus:v1.0.0" {
		t.Errorf("Image = %q, want %q", containers[0].Image, "gsoci.azurecr.io/giantswarm/klaus:v1.0.0")
	}

	// Verify configmap checksum annotation is set.
	if _, ok := dep.Spec.Template.Annotations["checksum/config"]; !ok {
		t.Error("expected checksum/config annotation on pod template")
	}

	// Verify security context.
	podSec := dep.Spec.Template.Spec.SecurityContext
	if podSec == nil {
		t.Fatal("expected pod security context")
	}
	if *podSec.RunAsUser != 1000 {
		t.Errorf("RunAsUser = %d, want 1000", *podSec.RunAsUser)
	}

	// Verify probes.
	if containers[0].LivenessProbe == nil {
		t.Error("expected liveness probe")
	}
	if containers[0].ReadinessProbe == nil {
		t.Error("expected readiness probe")
	}
}

func TestBuildDeployment_WithPlugins(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Plugins: []klausv1alpha1.PluginReference{
				{Repository: "registry.io/plugins/gs-base", Tag: "v1.0.0"},
			},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", DefaultGitCloneImage, nil)

	// Verify plugin volume exists.
	foundVolume := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "plugin-gs-base" {
			foundVolume = true
			if v.Image == nil {
				t.Error("expected image volume source for plugin")
			}
		}
	}
	if !foundVolume {
		t.Error("expected plugin-gs-base volume")
	}

	// Verify plugin volume mount exists.
	foundMount := false
	for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == "plugin-gs-base" {
			foundMount = true
		}
	}
	if !foundMount {
		t.Error("expected plugin-gs-base volume mount")
	}
}

func TestBuildDeployment_WithImagePullSecrets(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:            "user@example.com",
			ImagePullSecrets: []string{"my-registry-creds"},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", DefaultGitCloneImage, nil)

	pullSecrets := dep.Spec.Template.Spec.ImagePullSecrets
	if len(pullSecrets) != 1 {
		t.Fatalf("expected 1 pull secret, got %d", len(pullSecrets))
	}
	if pullSecrets[0].Name != "my-registry-creds" {
		t.Errorf("pull secret name = %q, want %q", pullSecrets[0].Name, "my-registry-creds")
	}
}

func TestBuildDeployment_WithWorkspace(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:     "user@example.com",
			Workspace: &klausv1alpha1.WorkspaceConfig{},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", DefaultGitCloneImage, nil)

	// Verify workspace volume.
	foundVolume := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == WorkspaceVolumeName {
			foundVolume = true
			if v.PersistentVolumeClaim == nil {
				t.Error("expected PVC volume source for workspace")
			}
		}
	}
	if !foundVolume {
		t.Error("expected workspace volume")
	}

	// Verify workspace mount.
	foundMount := false
	for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == WorkspaceVolumeName && m.MountPath == WorkspaceMountPath {
			foundMount = true
		}
	}
	if !foundMount {
		t.Error("expected workspace volume mount at " + WorkspaceMountPath)
	}
}

func TestBuildDeployment_WithCustomImage(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Image: "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		},
	}

	// The reconciler passes the resolved image to BuildDeployment.
	resolvedImage := instance.Spec.Image
	dep := BuildDeployment(instance, "klaus-user-test", resolvedImage, DefaultGitCloneImage, nil)

	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	if containers[0].Image != "gsoci.azurecr.io/giantswarm/klaus-go:1.25" {
		t.Errorf("Image = %q, want %q", containers[0].Image, "gsoci.azurecr.io/giantswarm/klaus-go:1.25")
	}
}

func TestBuildDeployment_SelectorLabelsMatchPodLabels(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "my-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "owner@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				PersistentMode: ptr.To(true),
			},
		},
	}

	dep := BuildDeployment(instance, "ns", "img:latest", DefaultGitCloneImage, nil)

	selectorLabels := SelectorLabels(instance)
	for k, v := range dep.Spec.Selector.MatchLabels {
		if selectorLabels[k] != v {
			t.Errorf("Deployment selector label %q=%q doesn't match SelectorLabels()", k, v)
		}
	}
	for k, v := range dep.Spec.Template.Labels {
		// Pod labels are a superset of selector labels.
		if sv, ok := selectorLabels[k]; ok && sv != v {
			t.Errorf("Pod template label %q=%q doesn't match SelectorLabels() value %q", k, v, sv)
		}
	}
}

func TestBuildDeployment_WithGitClone(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Workspace: &klausv1alpha1.WorkspaceConfig{
				GitRepo: "https://github.com/example/project.git",
				GitRef:  "main",
			},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", DefaultGitCloneImage, nil)

	// Verify init container exists.
	initContainers := dep.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(initContainers))
	}
	if initContainers[0].Name != "git-clone" {
		t.Errorf("init container name = %q, want %q", initContainers[0].Name, "git-clone")
	}
	if initContainers[0].Image != DefaultGitCloneImage {
		t.Errorf("init container image = %q, want %q", initContainers[0].Image, DefaultGitCloneImage)
	}

	// Verify workspace volume mount on init container.
	foundWorkspaceMount := false
	for _, m := range initContainers[0].VolumeMounts {
		if m.Name == WorkspaceVolumeName && m.MountPath == WorkspaceMountPath {
			foundWorkspaceMount = true
		}
	}
	if !foundWorkspaceMount {
		t.Error("expected workspace volume mount on git-clone init container")
	}

	// Verify no git secret mount (no gitSecretRef).
	for _, m := range initContainers[0].VolumeMounts {
		if m.Name == GitSecretVolumeName {
			t.Error("unexpected git secret volume mount when gitSecretRef is not set")
		}
	}

	// Verify security context on init container.
	sec := initContainers[0].SecurityContext
	if sec == nil {
		t.Fatal("expected security context on init container")
	}
	if *sec.RunAsUser != 1000 {
		t.Errorf("init container RunAsUser = %d, want 1000", *sec.RunAsUser)
	}
	if *sec.AllowPrivilegeEscalation {
		t.Error("init container should not allow privilege escalation")
	}
}

func TestBuildDeployment_WithGitCloneAndSecret(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Workspace: &klausv1alpha1.WorkspaceConfig{
				GitRepo: "git@github.com:example/project.git",
				GitRef:  "develop",
				GitSecretRef: &klausv1alpha1.GitSecretReference{
					Name: "github-deploy-key",
				},
			},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", DefaultGitCloneImage, nil)

	// Verify init container exists with git secret mount.
	initContainers := dep.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(initContainers))
	}

	foundGitSecretMount := false
	for _, m := range initContainers[0].VolumeMounts {
		if m.Name == GitSecretVolumeName && m.MountPath == GitSecretMountPath {
			foundGitSecretMount = true
			if !m.ReadOnly {
				t.Error("git secret mount should be read-only")
			}
		}
	}
	if !foundGitSecretMount {
		t.Error("expected git secret volume mount on git-clone init container")
	}

	// Verify git secret volume exists.
	foundSecretVolume := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == GitSecretVolumeName {
			foundSecretVolume = true
			if v.Secret == nil {
				t.Error("expected Secret volume source for git-secret")
			} else if v.Secret.SecretName != "test-instance-git-creds" {
				t.Errorf("secret name = %q, want %q", v.Secret.SecretName, "test-instance-git-creds")
			}
		}
	}
	if !foundSecretVolume {
		t.Error("expected git-secret volume")
	}

	// Verify the script contains SSH configuration.
	script := initContainers[0].Args[0]
	if !strings.Contains(script, "GIT_SSH_COMMAND") {
		t.Error("expected GIT_SSH_COMMAND in clone script when gitSecretRef is set")
	}
	if !strings.Contains(script, "ssh-privatekey") {
		t.Error("expected default secret key 'ssh-privatekey' in clone script")
	}
}

func TestBuildDeployment_WithGitCloneCustomKey(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Workspace: &klausv1alpha1.WorkspaceConfig{
				GitRepo: "git@github.com:example/project.git",
				GitSecretRef: &klausv1alpha1.GitSecretReference{
					Name: "my-key",
					Key:  "id_ed25519",
				},
			},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", DefaultGitCloneImage, nil)

	initContainers := dep.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(initContainers))
	}

	script := initContainers[0].Args[0]
	if !strings.Contains(script, "id_ed25519") {
		t.Error("expected custom key 'id_ed25519' in clone script")
	}
}

func TestBuildDeployment_NoGitCloneWithoutRepo(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:     "user@example.com",
			Workspace: &klausv1alpha1.WorkspaceConfig{},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", DefaultGitCloneImage, nil)

	if len(dep.Spec.Template.Spec.InitContainers) != 0 {
		t.Error("expected no init containers when workspace has no gitRepo")
	}
}

func TestBuildGitCloneScript_WithRef(t *testing.T) {
	script := buildGitCloneScript("https://github.com/example/project.git", "main", false, "")
	if !strings.Contains(script, "--branch 'main'") {
		t.Error("expected --branch 'main' (quoted) in clone script")
	}
	if !strings.Contains(script, "git checkout 'main'") {
		t.Error("expected git checkout 'main' (quoted) in update path")
	}
	if strings.Contains(script, "GIT_SSH_COMMAND") {
		t.Error("unexpected GIT_SSH_COMMAND when hasSecret is false")
	}
	if strings.Contains(script, "|| true") {
		t.Error("unexpected || true; should use warning echo instead")
	}
}

func TestBuildGitCloneScript_WithoutRef(t *testing.T) {
	script := buildGitCloneScript("https://github.com/example/project.git", "", false, "")
	if strings.Contains(script, "--branch") {
		t.Error("unexpected --branch when gitRef is empty")
	}
	if strings.Contains(script, "git checkout") {
		t.Error("unexpected git checkout when gitRef is empty")
	}
}

func TestBuildGitCloneScript_WithSecret(t *testing.T) {
	script := buildGitCloneScript("git@github.com:example/project.git", "main", true, "ssh-privatekey")
	if !strings.Contains(script, "GIT_SSH_COMMAND") {
		t.Error("expected GIT_SSH_COMMAND when hasSecret is true")
	}
	if !strings.Contains(script, "/etc/git-secret/ssh-privatekey") {
		t.Error("expected secret key path in GIT_SSH_COMMAND")
	}
	if !strings.Contains(script, "StrictHostKeyChecking=accept-new") {
		t.Error("expected StrictHostKeyChecking=accept-new in SSH command")
	}
}

func TestBuildGitCloneScript_ValuesAreShellQuoted(t *testing.T) {
	script := buildGitCloneScript("https://example.com/repo.git", "main", false, "")
	if !strings.Contains(script, "'https://example.com/repo.git'") {
		t.Error("expected gitRepo to be single-quoted in clone script")
	}
	if !strings.Contains(script, "'main'") {
		t.Error("expected gitRef to be single-quoted in clone script")
	}
}
