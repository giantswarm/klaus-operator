package oci

import (
	"testing"

	klausoci "github.com/giantswarm/klaus-oci"
)

func TestParsePersonalitySpec(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    PersonalitySpec
		wantErr bool
	}{
		{
			name: "full personality",
			yaml: `
description: "Go developer personality"
image: "gsoci.azurecr.io/giantswarm/klaus-go:latest"
systemPrompt: "You are an expert Go developer."
appendSystemPrompt: "Always follow Go conventions."
plugins:
  - repository: "gsoci.azurecr.io/giantswarm/plugin-gopls"
    tag: "v1.0.0"
  - repository: "gsoci.azurecr.io/giantswarm/plugin-gotools"
    digest: "sha256:abc123"
soul: |
  # SOUL
  I am a Go expert.
`,
			want: PersonalitySpec{
				Description:        "Go developer personality",
				Image:              "gsoci.azurecr.io/giantswarm/klaus-go:latest",
				SystemPrompt:       "You are an expert Go developer.",
				AppendSystemPrompt: "Always follow Go conventions.",
				Plugins: []klausoci.PluginReference{
					{Repository: "gsoci.azurecr.io/giantswarm/plugin-gopls", Tag: "v1.0.0"},
					{Repository: "gsoci.azurecr.io/giantswarm/plugin-gotools", Digest: "sha256:abc123"},
				},
				Soul: "# SOUL\nI am a Go expert.\n",
			},
		},
		{
			name: "minimal personality",
			yaml: `
description: "minimal"
`,
			want: PersonalitySpec{
				Description: "minimal",
			},
		},
		{
			name:    "invalid yaml",
			yaml:    `{invalid: [yaml`,
			wantErr: true,
		},
		{
			name: "empty personality",
			yaml: ``,
			want: PersonalitySpec{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePersonalitySpec([]byte(tt.yaml))
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePersonalitySpec() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("ParsePersonalitySpec() unexpected error: %v", err)
				return
			}

			if got.Description != tt.want.Description {
				t.Errorf("Description: got %q, want %q", got.Description, tt.want.Description)
			}
			if got.Image != tt.want.Image {
				t.Errorf("Image: got %q, want %q", got.Image, tt.want.Image)
			}
			if got.SystemPrompt != tt.want.SystemPrompt {
				t.Errorf("SystemPrompt: got %q, want %q", got.SystemPrompt, tt.want.SystemPrompt)
			}
			if got.AppendSystemPrompt != tt.want.AppendSystemPrompt {
				t.Errorf("AppendSystemPrompt: got %q, want %q", got.AppendSystemPrompt, tt.want.AppendSystemPrompt)
			}
			if got.Soul != tt.want.Soul {
				t.Errorf("Soul: got %q, want %q", got.Soul, tt.want.Soul)
			}
			if len(got.Plugins) != len(tt.want.Plugins) {
				t.Errorf("Plugins: got %d, want %d", len(got.Plugins), len(tt.want.Plugins))
				return
			}
			for i, p := range got.Plugins {
				if p.Repository != tt.want.Plugins[i].Repository {
					t.Errorf("Plugins[%d].Repository: got %q, want %q", i, p.Repository, tt.want.Plugins[i].Repository)
				}
				if p.Tag != tt.want.Plugins[i].Tag {
					t.Errorf("Plugins[%d].Tag: got %q, want %q", i, p.Tag, tt.want.Plugins[i].Tag)
				}
				if p.Digest != tt.want.Plugins[i].Digest {
					t.Errorf("Plugins[%d].Digest: got %q, want %q", i, p.Digest, tt.want.Plugins[i].Digest)
				}
			}
		})
	}
}
