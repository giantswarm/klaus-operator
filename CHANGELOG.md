# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

- Git clone init container for workspace: when `workspace.gitRepo` is set, the operator prepends an init container that clones the repository into the workspace PVC before the main container starts. Supports incremental updates on restarts (#16).
- `workspace.gitSecretRef` field on KlausInstance for private repository cloning via HTTPS access tokens (PAT or fine-grained token). The operator copies the referenced Secret (preserving its type) to the user namespace and injects the token into the clone URL (#16).
- `--git-clone-image` CLI flag to configure the init container image for workspace git clones (defaults to `alpine/git:v2.47.2`).
- `gitCloneImage` Helm chart value to configure the init container image via the operator deployment.
- `HOME=/tmp` and `GIT_CONFIG_NOSYSTEM=1` environment variables on the git-clone init container, with a writable `/tmp` emptyDir volume for git scratch space when running with `ReadOnlyRootFilesystem: true`.
- CRD validation patterns on `workspace.gitRepo`, `workspace.gitRef`, and `gitSecretRef.key` to reject shell metacharacters.
- Spec validation: `workspace.gitSecretRef` now requires `workspace.gitRepo` to be set.
- Kubernetes events for git credential copy and workspace clone configuration for improved observability.
- KlausMCPServer CRD (`klaus.giantswarm.io/v1alpha1`) for shared MCP server configuration with Secret injection (#5).
- KlausMCPServer controller with spec validation, Secret existence checks, status conditions (`Ready`, `SecretsValid`), and instance count tracking.
- KlausInstance controller resolves `mcpServers` refs, copies Secrets to user namespaces, and merges resolved configs with Secret dedup.
- KlausInstance controller watches KlausMCPServer changes and re-reconciles all referencing instances.
- MCP server readiness gate: instance controller checks `Ready` condition on referenced KlausMCPServers and fails fast with a clear message when a server is misconfigured.
- Secret name collision detection across MCP servers to prevent silent overwrites in user namespaces.
- Stale MCP secret cleanup: reconciler removes orphaned MCP secrets from user namespaces when references change, respecting multi-instance ownership.
- Field indexer (`spec.mcpServers.name`) for efficient instance lookups by MCP server name, replacing full list scans.
- Override events: informational events emitted when a KlausMCPServer overrides an inline `claude.mcpServers` entry with the same name.
- `${VAR}` expansion documented in CRD field descriptions for `env` and `headers`.
- Unit tests for MCP server config marshaling, merge semantics, and secret deduplication.
- OCI-based personality support: `KlausInstance.spec.personality` accepts an OCI artifact reference (e.g., `gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v1.0.0`) containing `personality.yaml` and `SOUL.md` (#20).
- ORAS client (`internal/oci`) for pulling personality artifacts from OCI registries with digest-based caching and Kubernetes `imagePullSecrets` auth.
- SOUL.md from personality artifacts mounted into the container via ConfigMap at `/etc/klaus/SOUL.md`.
- Merge logic for OCI personalities: image (instance overrides personality), plugins (deduplicated by repository, instance version wins), system prompts (instance overrides personality).
- Initial repository setup from giantswarm/template.
- KlausInstance CRD (`klaus.giantswarm.io/v1alpha1`) with full configuration surface matching the standalone Helm chart.
- KlausInstance controller reconciling to Namespace, Deployment, Service, PVC, ConfigMap, Secret, and MCPServer CRD.
- Resource rendering package mirroring Helm chart patterns: env vars, ConfigMap (skills, hooks, MCP config, agents), OCI image volumes for plugins, volume mounts.
- MCP server interface with 5 tools: `create_instance`, `list_instances`, `delete_instance`, `get_instance`, `restart_instance`.
- Credential strategy: shared Anthropic API key Secret copied to user namespaces.
- Helm chart for the operator with CRD, RBAC, Deployment, Service, ServiceAccount, and static MCPServer registration.
- Multi-stage Dockerfile based on distroless.
- Unit tests for resource rendering, ConfigMap building, env var generation, and JWT auth extraction.
- MCP server JWT auth via `WithHTTPContextFunc`: Authorization header injected into context for tool handler user extraction.
- Spec validation: mutual-exclusivity checks for hooks vs settingsFile, plugin tag vs digest, and plugin short name uniqueness.
- `pluginDirs` field on KlausInstance spec for user-provided plugin directory paths, merged with OCI mount paths into `CLAUDE_PLUGIN_DIRS`.
- `imagePullSecrets` field on KlausInstance spec for private registry authentication on instance pods.
- Status conditions (`Ready`, `ConfigReady`, `DeploymentReady`, `MCPServerReady`) populated during reconciliation.
- Stub CRD for KlausMCPServer in the Helm chart (full implementation in #5).

### Changed

- Git clone init container image pinned to `alpine/git:v2.47.2` instead of `:latest` for reproducible deployments; configurable via `--git-clone-image`.
- Git clone shell script now single-quotes user-supplied `gitRepo`, `gitRef`, and workspace path values as defense-in-depth against shell injection.
- Git clone update path now uses separate fetch/checkout/pull commands with explicit error handling instead of a single `&&` chain, improving readability and making failure behavior per-command.
- Git clone update path now logs a warning instead of silently succeeding when `git pull` fails (`|| true` replaced with `|| echo WARNING`).
- Git clone authentication changed from SSH (`GIT_SSH_COMMAND`) to HTTPS token-based auth via `x-access-token` URL injection; credentials are stripped from the persisted remote URL after clone/fetch.
- Default git secret key changed from `ssh-privatekey` to `token` to reflect HTTPS token auth.
- CRDs regenerated with controller-gen to include validation patterns and updated field descriptions.
- KlausMCPServer short name changed from `kms` to `kmcp` to avoid confusion with AWS KMS.
- KlausMCPServer CRD description now documents merge semantics (resolved config takes precedence over inline entries with the same name).

- Replaced manual get-create-or-update methods with `controllerutil.CreateOrUpdate` across all reconciled resources (Namespace, Secret, ConfigMap, Deployment, Service, ServiceAccount).
- Replaced custom `setCondition` implementation with `apimeta.SetStatusCondition` from `k8s.io/apimachinery`.
- Extracted `SelectorLabels` helper to ensure Deployment and Service selector labels are always in sync.
- Extracted `mcpServerGVK` package-level variable to eliminate duplicated MCPServer GVK construction.
- Typed `PermissionMode`, `EffortLevel`, and `InstanceMode` as named string types with `kubebuilder:validation:Enum` markers.
- Changed `SkillConfig.AllowedTools` from `string` to `[]string` for consistency with `ClaudeConfig.AllowedTools`.
- Added CEL validation markers on `PluginReference` for tag/digest mutual exclusivity.
- Deployment restart in MCP tools now uses `client.MergeFrom` patch instead of full `Update` to avoid conflicts.
- Controller returns early after adding finalizer for cleaner reconcile loop.
- Controller now checks `Deployment.Status.AvailableReplicas` before setting `Running` state; uses `Pending` + requeue while rolling out.
- Dockerfile now uses `-trimpath -ldflags="-s -w"` for smaller, reproducible builds.
- Added `.dockerignore` to exclude `.git`, docs, and Helm chart from build context.
- MCPServer CRD `toolPrefix` field is now only included when non-empty.
- Removed redundant per-item `Mode` from hook script volume items (covered by `DefaultMode`).
- Simplified `sanitizeIdentifier` by removing redundant `@` and `.` replacements already handled by the regex.
- Fixed `sanitizeIdentifier` to trim hyphens after truncation to prevent invalid trailing-hyphen DNS labels.
- Fixed `compactJSON` for skill context to prevent multi-line JSON from breaking YAML frontmatter.
- MCP server is now wired through controller-runtime's manager for graceful shutdown instead of a bare goroutine with `os.Exit`.
- Replaced `copyAPIKeySecret` dead `[]byte` return with `(bool, error)` for clarity.
- Unified `UserNamespace` and `sanitizeLabelValue` into a shared `sanitizeIdentifier` helper.
- Extracted `getOwnedInstance` helper in MCP tool handlers to reduce repetition.
- Replaced manual sort-map-keys helpers with `slices.Sorted(maps.Keys(...))` (Go 1.25).
- Unified `buildMCPConfigJSON`, `buildAgentsJSON`, and `buildHooksJSON` into a single `marshalRawExtensionMap` function.
- Extracted `MusterNamespace` helper to eliminate duplicated namespace resolution logic.
- `ServiceEndpoint` now uses `KlausPort` constant instead of a hardcoded `"8080"` string.
- `ensureNamespace` now reconciles labels on existing namespaces, fixing stale owner labels.

### Fixed

- Fixed `reconcileDelete` to collect all child deletion errors and only remove the finalizer when all resources are confirmed deleted, preventing resource orphaning.
- Fixed `Personality` status field not being cleared when personality reference is removed from the spec.
- Fixed potential panic in `reconcileMCPServer` from unsafe type assertion on unstructured metadata; replaced with `SetLabels`/`GetLabels`.
- Fixed case-insensitive Bearer token stripping per RFC 6750; previously only matched `Bearer` and `bearer`.
- Cross-namespace resource management: replaced `Owns()` with label-based watches using `builder.WithPredicates` and `LabelSelectorPredicate`, since owner references cannot cross namespace boundaries.
- Deletion now cleans up all in-namespace resources (Deployment, Service, ConfigMap, Secret, ServiceAccount, PVC) in addition to the cross-namespace MCPServer CRD.

### Removed

- Removed unused exported `MarshalJSONMap` function.



[Unreleased]: https://github.com/giantswarm/klaus-operator/tree/main
