package resources

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestValidateSpec_HooksExclusivity(t *testing.T) {
	tests := []struct {
		name    string
		spec    klausv1alpha1.KlausInstanceSpec
		wantErr string
	}{
		{
			name: "hooks only -- valid",
			spec: klausv1alpha1.KlausInstanceSpec{
				Owner: "user@example.com",
				Hooks: map[string]runtime.RawExtension{
					"PreToolUse": {Raw: []byte(`[]`)},
				},
			},
		},
		{
			name: "settingsFile only -- valid",
			spec: klausv1alpha1.KlausInstanceSpec{
				Owner: "user@example.com",
				Claude: klausv1alpha1.ClaudeConfig{
					SettingsFile: "/custom/settings.json",
				},
			},
		},
		{
			name: "both hooks and settingsFile -- invalid",
			spec: klausv1alpha1.KlausInstanceSpec{
				Owner: "user@example.com",
				Hooks: map[string]runtime.RawExtension{
					"PreToolUse": {Raw: []byte(`[]`)},
				},
				Claude: klausv1alpha1.ClaudeConfig{
					SettingsFile: "/custom/settings.json",
				},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "neither -- valid",
			spec: klausv1alpha1.KlausInstanceSpec{
				Owner: "user@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &klausv1alpha1.KlausInstance{Spec: tt.spec}
			err := ValidateSpec(instance)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateSpec_PluginTagDigest(t *testing.T) {
	tests := []struct {
		name    string
		plugins []klausv1alpha1.PluginReference
		wantErr string
	}{
		{
			name: "tag only -- valid",
			plugins: []klausv1alpha1.PluginReference{
				{Repository: "reg.io/plugins/base", Tag: "v1.0.0"},
			},
		},
		{
			name: "digest only -- valid",
			plugins: []klausv1alpha1.PluginReference{
				{Repository: "reg.io/plugins/base", Digest: "sha256:abc123"},
			},
		},
		{
			name: "both tag and digest -- invalid",
			plugins: []klausv1alpha1.PluginReference{
				{Repository: "reg.io/plugins/base", Tag: "v1.0.0", Digest: "sha256:abc123"},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "neither tag nor digest -- invalid",
			plugins: []klausv1alpha1.PluginReference{
				{Repository: "reg.io/plugins/base"},
			},
			wantErr: "must specify either tag or digest",
		},
		{
			name: "bad digest format",
			plugins: []klausv1alpha1.PluginReference{
				{Repository: "reg.io/plugins/base", Digest: "md5:abc"},
			},
			wantErr: "must start with 'sha256:'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &klausv1alpha1.KlausInstance{
				Spec: klausv1alpha1.KlausInstanceSpec{
					Owner:   "user@example.com",
					Plugins: tt.plugins,
				},
			}
			err := ValidateSpec(instance)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateSpec_PluginShortNameUniqueness(t *testing.T) {
	tests := []struct {
		name    string
		plugins []klausv1alpha1.PluginReference
		wantErr string
	}{
		{
			name: "unique short names",
			plugins: []klausv1alpha1.PluginReference{
				{Repository: "reg.io/plugins/base", Tag: "v1.0.0"},
				{Repository: "reg.io/plugins/security", Tag: "v0.5.0"},
			},
		},
		{
			name: "conflicting short names",
			plugins: []klausv1alpha1.PluginReference{
				{Repository: "reg.io/teamA/plugins/base", Tag: "v1.0.0"},
				{Repository: "reg.io/teamB/plugins/base", Tag: "v2.0.0"},
			},
			wantErr: "short name \"base\" conflicts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &klausv1alpha1.KlausInstance{
				Spec: klausv1alpha1.KlausInstanceSpec{
					Owner:   "user@example.com",
					Plugins: tt.plugins,
				},
			}
			err := ValidateSpec(instance)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
