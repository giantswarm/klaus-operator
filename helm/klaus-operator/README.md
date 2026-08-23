# klaus-operator

![Version: [[ .Version ]]](https://img.shields.io/badge/Version-[[ .Version ]]-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: [[ .AppVersion ]]](https://img.shields.io/badge/AppVersion-[[ .AppVersion ]]-informational?style=flat-square)

Kubernetes operator for dynamic management of Klaus AI agent instances

**Homepage:** <https://github.com/giantswarm/klaus-operator>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Giant Swarm |  | <https://www.giantswarm.io> |

## Source Code

* <https://github.com/giantswarm/klaus-operator>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| name | string | `"klaus-operator"` |  |
| serviceType | string | `"managed"` |  |
| image.registry | string | `"gsoci.azurecr.io"` |  |
| image.name | string | `"giantswarm/klaus-operator"` |  |
| image.tag | string | `""` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| klausImage | string | `"gsoci.azurecr.io/giantswarm/klaus:latest"` |  |
| gitCloneImage | string | `"alpine/git:v2.54.0"` |  |
| replicaCount | int | `1` |  |
| leaderElection.enabled | bool | `false` |  |
| anthropicKeySecret.name | string | `"anthropic-api-key"` |  |
| anthropicKeySecret.namespace | string | `""` |  |
| mcp.port | int | `9090` |  |
| muster.namespace | string | `"muster"` |  |
| muster.registerOperator | bool | `true` |  |
| pod.user.id | int | `65532` |  |
| pod.group.id | int | `65532` |  |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"128Mi"` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.annotations | object | `{}` |  |
| metrics.enabled | bool | `true` |  |
| metrics.port | int | `8080` |  |
| probes.port | int | `8081` |  |
| serviceMonitor.enabled | bool | `true` |  |
| serviceMonitor.interval | string | `"60s"` |  |
| serviceMonitor.scrapeTimeout | string | `"45s"` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.readOnlyRootFilesystem | bool | `true` |  |
| securityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| global.podSecurityStandards.enforced | bool | `false` |  |
| nodeSelector | object | `{}` |  |
| tolerations | list | `[]` |  |
| affinity | object | `{}` |  |
