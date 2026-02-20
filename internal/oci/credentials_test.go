package oci

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"oras.land/oras-go/v2/registry/remote/auth"
)

func TestParseDockerConfigSecret(t *testing.T) {
	makeAuthString := func(user, pass string) string {
		return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	}

	makeDockerConfigJSON := func(registry, username, password string) []byte {
		cfg := map[string]interface{}{
			"auths": map[string]interface{}{
				registry: map[string]string{
					"username": username,
					"password": password,
				},
			},
		}
		b, _ := json.Marshal(cfg)
		return b
	}

	makeDockerConfigJSONWithAuth := func(registry, user, pass string) []byte {
		cfg := map[string]interface{}{
			"auths": map[string]interface{}{
				registry: map[string]string{
					"auth": makeAuthString(user, pass),
				},
			},
		}
		b, _ := json.Marshal(cfg)
		return b
	}

	tests := []struct {
		name    string
		secret  *corev1.Secret
		wantErr bool
		wantLen int
		checks  []func(t *testing.T, entries []credEntry)
	}{
		{
			name: "dockerconfigjson with username/password",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "pull-secret"},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: makeDockerConfigJSON("myregistry.example.com", "user1", "pass1"),
				},
			},
			wantLen: 1,
			checks: []func(t *testing.T, entries []credEntry){
				func(t *testing.T, entries []credEntry) {
					if entries[0].host != "myregistry.example.com" {
						t.Errorf("host: got %q, want %q", entries[0].host, "myregistry.example.com")
					}
					if entries[0].cred.Username != "user1" {
						t.Errorf("username: got %q, want %q", entries[0].cred.Username, "user1")
					}
					if entries[0].cred.Password != "pass1" {
						t.Errorf("password: got %q, want %q", entries[0].cred.Password, "pass1")
					}
				},
			},
		},
		{
			name: "dockerconfigjson with auth field",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "pull-secret-auth"},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: makeDockerConfigJSONWithAuth("gsoci.azurecr.io", "myuser", "mypassword"),
				},
			},
			wantLen: 1,
			checks: []func(t *testing.T, entries []credEntry){
				func(t *testing.T, entries []credEntry) {
					if entries[0].host != "gsoci.azurecr.io" {
						t.Errorf("host: got %q, want %q", entries[0].host, "gsoci.azurecr.io")
					}
					if entries[0].cred.Username != "myuser" {
						t.Errorf("username: got %q, want %q", entries[0].cred.Username, "myuser")
					}
					if entries[0].cred.Password != "mypassword" {
						t.Errorf("password: got %q, want %q", entries[0].cred.Password, "mypassword")
					}
				},
			},
		},
		{
			name: "unsupported secret type",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "generic-secret"},
				Type:       corev1.SecretTypeOpaque,
				Data:       map[string][]byte{"key": []byte("value")},
			},
			wantErr: true,
		},
		{
			name: "invalid auth base64",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-secret"},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(fmt.Sprintf(`{"auths":{"registry.example.com":{"auth":"!!!not-base64!!!"}}}`)),
				},
			},
			wantErr: true,
		},
		{
			name: "empty auths",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-secret"},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`),
				},
			},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := parseDockerConfigSecret(tt.secret)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDockerConfigSecret() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("parseDockerConfigSecret() unexpected error: %v", err)
				return
			}
			if len(entries) != tt.wantLen {
				t.Errorf("len(entries): got %d, want %d", len(entries), tt.wantLen)
				return
			}
			for _, check := range tt.checks {
				check(t, entries)
			}
		})
	}
}

func TestBuildCredentials_EmptyPullSecrets(t *testing.T) {
	c := &Client{cache: make(map[string]*PersonalitySpec)}
	credFunc, err := c.buildCredentials(nil, nil, "")
	if err != nil {
		t.Fatalf("buildCredentials() unexpected error: %v", err)
	}
	cred, err := credFunc(nil, "registry.example.com")
	if err != nil {
		t.Fatalf("credFunc() unexpected error: %v", err)
	}
	if cred != auth.EmptyCredential {
		t.Errorf("credFunc() got %+v, want EmptyCredential", cred)
	}
}
