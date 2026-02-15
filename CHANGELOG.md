# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

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
- Stub CRDs for KlausPersonality and KlausMCPServer in the Helm chart (full implementation in #3 and #5).

### Fixed

- Cross-namespace resource management: replaced `Owns()` with label-based watches using `builder.WithPredicates` and `LabelSelectorPredicate`, since owner references cannot cross namespace boundaries.
- Deletion now cleans up all in-namespace resources (Deployment, Service, ConfigMap, Secret, ServiceAccount, PVC) in addition to the cross-namespace MCPServer CRD.



[Unreleased]: https://github.com/giantswarm/klaus-operator/tree/main
