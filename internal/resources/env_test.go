package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestBuildEnvVars_Basics(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				Model:          "claude-sonnet-4-20250514",
				PermissionMode: klausv1alpha1.PermissionModeBypass,
			},
		},
	}

	envs := BuildEnvVars(instance, "test-config", "test-secret")

	// Check PORT is set.
	assertEnvValue(t, envs, "PORT", "8080")
	// Check model.
	assertEnvValue(t, envs, "CLAUDE_MODEL", "claude-sonnet-4-20250514")
	// Check permission mode.
	assertEnvValue(t, envs, "CLAUDE_PERMISSION_MODE", "bypassPermissions")
	// Check owner subject.
	assertEnvValue(t, envs, "KLAUS_OWNER_SUBJECT", "test@example.com")
	// Check ANTHROPIC_API_KEY is from secret.
	assertEnvFromSecret(t, envs, "ANTHROPIC_API_KEY", "test-secret", "api-key")
}

func TestBuildEnvVars_Plugins(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Plugins: []klausv1alpha1.PluginReference{
				{Repository: "registry.io/plugins/gs-base", Tag: "v1.0.0"},
				{Repository: "registry.io/plugins/security", Tag: "v0.5.0"},
			},
		},
	}

	envs := BuildEnvVars(instance, "test-config", "test-secret")

	assertEnvValue(t, envs, "CLAUDE_PLUGIN_DIRS", "/var/lib/klaus/plugins/gs-base,/var/lib/klaus/plugins/security")
}

func TestBuildEnvVars_AddDirsWithExtensions(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Skills: map[string]klausv1alpha1.SkillConfig{
				"test": {Content: "test"},
			},
			AddDirs: []string{"/extra/dir"},
		},
	}

	envs := BuildEnvVars(instance, "test-config", "test-secret")

	assertEnvValue(t, envs, "CLAUDE_ADD_DIRS", "/extra/dir,/etc/klaus/extensions")
	assertEnvValue(t, envs, "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD", "true")
}

func TestBuildEnvVars_Telemetry(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Telemetry: &klausv1alpha1.TelemetryConfig{
				Enabled:         ptr.To(true),
				MetricsExporter: "otlp",
				OTLP: &klausv1alpha1.OTLPConfig{
					Protocol: "grpc",
					Endpoint: "http://otel-collector:4317",
				},
			},
		},
	}

	envs := BuildEnvVars(instance, "test-config", "test-secret")

	assertEnvValue(t, envs, "CLAUDE_CODE_ENABLE_TELEMETRY", "1")
	assertEnvValue(t, envs, "OTEL_METRICS_EXPORTER", "otlp")
	assertEnvValue(t, envs, "OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	assertEnvValue(t, envs, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317")
}

func TestBuildEnvVars_PersistentMode(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "test@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				PersistentMode: ptr.To(true),
			},
		},
	}

	envs := BuildEnvVars(instance, "test-config", "test-secret")

	assertEnvValue(t, envs, "CLAUDE_PERSISTENT_MODE", "true")
}

func assertEnvValue(t *testing.T, envs []corev1.EnvVar, name, expectedValue string) {
	t.Helper()
	for _, env := range envs {
		if env.Name == name {
			if env.Value != expectedValue {
				t.Errorf("env %s = %q, want %q", name, env.Value, expectedValue)
			}
			return
		}
	}
	t.Errorf("env %s not found in env vars", name)
}

func assertEnvFromSecret(t *testing.T, envs []corev1.EnvVar, name, secretName, key string) {
	t.Helper()
	for _, env := range envs {
		if env.Name == name {
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Errorf("env %s should be from secret, but has no SecretKeyRef", name)
				return
			}
			if env.ValueFrom.SecretKeyRef.Name != secretName {
				t.Errorf("env %s secret name = %q, want %q", name, env.ValueFrom.SecretKeyRef.Name, secretName)
			}
			if env.ValueFrom.SecretKeyRef.Key != key {
				t.Errorf("env %s secret key = %q, want %q", name, env.ValueFrom.SecretKeyRef.Key, key)
			}
			return
		}
	}
	t.Errorf("env %s not found in env vars", name)
}
