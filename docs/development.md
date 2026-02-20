# Development

## Prerequisites

- Go 1.25+
- Docker (for building container images)
- A Kubernetes cluster (for integration testing)

## Project Structure

```
.
├── api/v1alpha1/          # CRD type definitions
│   ├── groupversion_info.go
│   ├── klausinstance_types.go
│   └── zz_generated.deepcopy.go
├── internal/
│   ├── controller/        # KlausInstance reconciler
│   ├── mcp/               # MCP server (streamable-http)
│   └── resources/         # Kubernetes resource rendering
├── helm/klaus-operator/   # Operator Helm chart
│   ├── crds/              # CRD manifests
│   └── templates/         # Chart templates
├── main.go                # Entry point
├── Dockerfile             # Multi-stage build
└── go.mod
```

## Building

```bash
go build ./...
```

## Testing

```bash
go test ./...
```

## Formatting

This project enforces `goimports` formatting with local import grouping:

```bash
goimports -local github.com/giantswarm/klaus-operator -w .
```

## Docker Image

```bash
docker build -t klaus-operator:dev .
```

## CRD Generation

After modifying types in `api/v1alpha1/`, regenerate the deepcopy functions:

```bash
controller-gen object paths="./api/..."
```

And regenerate the CRD manifests:

```bash
controller-gen crd paths="./api/..." output:crd:dir=helm/klaus-operator/crds
```

## Architecture

The operator follows the standard controller-runtime pattern:

1. **KlausInstance CRD** -- declares the desired state of a Klaus agent instance
2. **Controller** -- reconciles each KlausInstance into Kubernetes resources (Deployment, Service, ConfigMap, PVC, Secret, Namespace)
3. **MCP Server** -- exposes instance management tools (create, list, delete, get, restart) via streamable-http transport
4. **Resource Rendering** -- mirrors the standalone Klaus Helm chart patterns for env vars, volumes, and ConfigMap entries

### Resource Lifecycle

For each KlausInstance, the controller creates:

- `klaus-user-{owner}` namespace (one per user)
- ConfigMap with system prompts, MCP config, skills, hooks, agents
- PVC for workspace storage (optional)
- API key Secret (copied from shared org secret)
- ServiceAccount
- Deployment with full Klaus configuration
- Service (ClusterIP on port 8080)
- MCPServer CRD in muster namespace

### MCP Tools

The operator itself runs an MCP server registered in muster:

| Tool | Description |
|------|-------------|
| `create_instance` | Create a new Klaus instance for the calling user |
| `list_instances` | List the calling user's instances |
| `delete_instance` | Delete an instance (owner-only) |
| `get_instance` | Get instance details and status |
| `restart_instance` | Restart by cycling the Deployment |

### Related Issues

- #5 -- KlausMCPServer CRD (shared MCP server config with Secret injection)
- #6 -- OCI artifact format for plugins (moved to klausctl)
- #20 -- OCI-based personalities (replaces KlausPersonality CRD)
