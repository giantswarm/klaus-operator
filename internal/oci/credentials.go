package oci

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// dockerConfigJSON is the format of kubernetes.io/dockerconfigjson secrets.
type dockerConfigJSON struct {
	Auths map[string]dockerConfigEntry `json:"auths"`
}

// dockerConfigEntry holds authentication credentials for one registry.
type dockerConfigEntry struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Auth     string `json:"auth,omitempty"` // base64("user:password")
}

// credEntry associates a registry hostname with its auth credential.
type credEntry struct {
	host string
	cred auth.Credential
}

// parseDockerConfigSecret parses a Kubernetes imagePullSecret (of type
// kubernetes.io/dockerconfigjson or kubernetes.io/dockercfg) into a list of
// per-registry credential entries.
func parseDockerConfigSecret(secret *corev1.Secret) ([]credEntry, error) {
	var rawConfig []byte
	switch secret.Type {
	case corev1.SecretTypeDockerConfigJson:
		rawConfig = secret.Data[corev1.DockerConfigJsonKey]
	case corev1.SecretTypeDockercfg:
		// Legacy .dockercfg — wrap to make it compatible with dockerConfigJSON.
		raw := secret.Data[corev1.DockerConfigKey]
		rawConfig = []byte(`{"auths":` + string(raw) + `}`)
	default:
		return nil, fmt.Errorf("unsupported secret type %q; expected kubernetes.io/dockerconfigjson", secret.Type)
	}

	var config dockerConfigJSON
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshaling docker config: %w", err)
	}

	var entries []credEntry
	for registry, e := range config.Auths {
		cred := auth.Credential{}
		if e.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(e.Auth)
			if err != nil {
				return nil, fmt.Errorf("decoding auth for %q: %w", registry, err)
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid auth encoding for %q", registry)
			}
			cred.Username = parts[0]
			cred.Password = parts[1]
		} else {
			cred.Username = e.Username
			cred.Password = e.Password
		}
		entries = append(entries, credEntry{host: registry, cred: cred})
	}
	return entries, nil
}
