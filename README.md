# klaus-operator

Kubernetes operator for dynamic management of [Klaus](https://github.com/giantswarm/klaus) instances. Enables platform teams to define reusable agent configurations and lets development teams create on-demand headless AI coding agents via Custom Resource Definitions.

## Overview

The klaus-operator manages the full lifecycle of Klaus instances through Kubernetes CRDs:

- **KlausInstance** -- represents a running Klaus agent with its configuration, workspace, and MCP server registration
- **KlausPersonality** -- reusable templates combining plugins, skills, agents, hooks, MCP servers, and model settings
- **ClaudePlugin** -- shared Claude Code plugins (inline or OCI-sourced) that can be referenced by multiple instances
- **ClaudeMCPServer** -- shared MCP server configurations with Secret injection for credentials

## Architecture

```
User IDE  -->  Muster  -->  klaus-operator MCP  (create/list/delete instances)
                  |
                  +---->  klaus instance A  (prompt/status/stop/result)
                  +---->  klaus instance B  (prompt/status/stop/result)
```

The operator itself exposes an MCP server interface (registered in Muster) with tools for creating, listing, and managing instances. Each managed Klaus instance runs as a separate Deployment with its own PVC workspace.

## CRDs

| CRD | Description |
|-----|-------------|
| `KlausInstance` | A running Klaus agent instance with configuration and workspace |
| `KlausPersonality` | Reusable template for instance configuration |
| `ClaudePlugin` | Shared plugin definition (inline content or OCI artifact reference) |
| `ClaudeMCPServer` | Shared MCP server config with Secret-based credential injection |

## Development

See [docs/development.md](docs/development.md) for development setup and contribution guidelines.

## License

Apache 2.0 -- see [LICENSE](LICENSE).
