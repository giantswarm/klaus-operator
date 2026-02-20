package oci

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	klausoci "github.com/giantswarm/klaus-oci"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// maxExtractFileSize guards against decompression bombs.
	maxExtractFileSize = 10 << 20 // 10 MB

	// maxManifestSize limits the OCI manifest read to 1 MB.
	maxManifestSize = 1 << 20
)

// Client is an OCI client with digest-based in-memory caching.
// The cache is unbounded but safe for operators with a bounded set of
// personality digests. Consider adding LRU eviction if the number of
// distinct digests grows significantly.
type Client struct {
	k8s   client.Client
	mu    sync.Mutex
	cache map[string]*PersonalitySpec // digest -> parsed personality
}

// NewClient creates a new OCI client backed by the given Kubernetes client
// for imagePullSecrets resolution.
func NewClient(k8s client.Client) *Client {
	return &Client{
		k8s:   k8s,
		cache: make(map[string]*PersonalitySpec),
	}
}

// PullPersonality pulls a personality OCI artifact, extracts personality.yaml
// and SOUL.md from the content layer tar.gz, and returns a parsed
// PersonalitySpec. Results are cached by manifest digest.
func (c *Client) PullPersonality(ctx context.Context, ref string, pullSecrets []string, secretNamespace string) (*PersonalitySpec, error) {
	credFunc, err := c.buildCredentials(ctx, pullSecrets, secretNamespace)
	if err != nil {
		return nil, err
	}

	repo, err := remoteRepo(ref, credFunc)
	if err != nil {
		return nil, err
	}

	refPart := repo.Reference.Reference

	desc, err := repo.Resolve(ctx, refPart)
	if err != nil {
		return nil, fmt.Errorf("resolving %q: %w", ref, err)
	}

	cacheKey := string(desc.Digest)
	c.mu.Lock()
	if cached, ok := c.cache[cacheKey]; ok {
		c.mu.Unlock()
		return cached.copy(), nil
	}
	c.mu.Unlock()

	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest for %q: %w", ref, err)
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(rc, maxManifestSize))
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("reading manifest for %q: %w", ref, err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest for %q: %w", ref, err)
	}

	// Find the content layer by media type.
	var contentLayer *ocispec.Descriptor
	for i := range manifest.Layers {
		if manifest.Layers[i].MediaType == klausoci.MediaTypePersonalityContent {
			contentLayer = &manifest.Layers[i]
			break
		}
	}
	if contentLayer == nil {
		return nil, fmt.Errorf("personality content layer not found in OCI artifact %q", ref)
	}

	blobRC, err := repo.Blobs().Fetch(ctx, *contentLayer)
	if err != nil {
		return nil, fmt.Errorf("fetching content layer from %q: %w", ref, err)
	}
	defer blobRC.Close()

	files, err := extractTarGz(blobRC, "personality.yaml", "SOUL.md")
	if err != nil {
		return nil, fmt.Errorf("extracting content layer from %q: %w", ref, err)
	}

	specData, ok := files["personality.yaml"]
	if !ok {
		return nil, fmt.Errorf("personality.yaml not found in content layer of %q", ref)
	}

	spec, err := ParsePersonalitySpec(specData)
	if err != nil {
		return nil, err
	}

	// SOUL.md file takes precedence over any "soul" field in personality.yaml.
	if soul, ok := files["SOUL.md"]; ok {
		spec.Soul = string(soul)
	}

	c.mu.Lock()
	c.cache[cacheKey] = spec
	c.mu.Unlock()

	return spec.copy(), nil
}

// extractTarGz reads a gzipped tar stream and returns the contents of the
// named files. Entries not in the wanted set are skipped. Returns early once
// all wanted files have been found.
func extractTarGz(r io.Reader, wanted ...string) (map[string][]byte, error) {
	wantSet := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		wantSet[w] = true
	}

	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("decompressing: %w", err)
	}
	defer gz.Close()

	result := make(map[string][]byte, len(wanted))
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}

		name := cleanTarPath(hdr.Name)
		if !wantSet[name] {
			continue
		}

		if hdr.Size > maxExtractFileSize {
			return nil, fmt.Errorf("file %q exceeds size limit (%d > %d)", name, hdr.Size, maxExtractFileSize)
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxExtractFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", name, err)
		}
		if int64(len(data)) > maxExtractFileSize {
			return nil, fmt.Errorf("file %q exceeds size limit", name)
		}

		result[name] = data

		if len(result) == len(wantSet) {
			break
		}
	}

	return result, nil
}

// cleanTarPath normalises tar entry names so that entries like
// "./personality.yaml" or "/personality.yaml" match "personality.yaml".
func cleanTarPath(name string) string {
	return strings.TrimPrefix(path.Clean(name), "/")
}

// remoteRepo opens an oras remote.Repository for the given OCI reference
// configured with the provided credential function.
func remoteRepo(ref string, credFunc auth.CredentialFunc) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("creating OCI repository client for %q: %w", ref, err)
	}
	repo.Client = &auth.Client{
		Credential: credFunc,
	}
	return repo, nil
}

// buildCredentials returns an auth.CredentialFunc that resolves credentials
// from the given Kubernetes imagePullSecrets.
func (c *Client) buildCredentials(ctx context.Context, pullSecrets []string, secretNamespace string) (auth.CredentialFunc, error) {
	var entries []credEntry

	for _, secretName := range pullSecrets {
		var secret corev1.Secret
		if err := c.k8s.Get(ctx, types.NamespacedName{
			Name:      secretName,
			Namespace: secretNamespace,
		}, &secret); err != nil {
			return nil, fmt.Errorf("fetching pull secret %q: %w", secretName, err)
		}
		parsed, err := parseDockerConfigSecret(&secret)
		if err != nil {
			return nil, fmt.Errorf("parsing pull secret %q: %w", secretName, err)
		}
		entries = append(entries, parsed...)
	}

	return func(_ context.Context, hostport string) (auth.Credential, error) {
		for _, e := range entries {
			if e.host == hostport {
				return e.cred, nil
			}
		}
		return auth.EmptyCredential, nil
	}, nil
}
