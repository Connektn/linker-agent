# Connektn Helm Charts

Kubernetes deployment manifests for the Connektn privacy-preserving billing reconciliation platform.

## Charts

This repository contains three Helm charts:

### 1. connektn-agent

The core Linker Agent that performs privacy-preserving reconciliation between usage and billing data.

- **Chart Version**: 0.1.0
- **App Version**: 0.1.0
- **Type**: Application
- [Documentation](connektn-agent/README.md)

**Key Features:**
- Supports Lite (embedded WAL) and HA (Kafka/RabbitMQ) profiles
- Zero-knowledge privacy architecture
- Configurable matcher pipeline
- Health and readiness probes
- Prometheus metrics support

### 2. connektn-gateway

NGINX/Envoy-based gateway adapter providing secure ingress, TLS termination, and rate limiting.

- **Chart Version**: 0.1.0
- **App Version**: 1.25.0 (NGINX)
- **Type**: Application
- [Documentation](connektn-gateway/README.md)

**Key Features:**
- TLS/mTLS termination
- Per-IP rate limiting
- Request routing
- JSON access logs
- Health check proxying

### 3. connektn-stack

Umbrella chart that deploys the complete Connektn stack (agent + gateway).

- **Chart Version**: 0.1.0
- **App Version**: 0.1.0
- **Type**: Application
- [Documentation](connektn-stack/README.md)

**Key Features:**
- One-command deployment
- Pre-configured for Lite and HA profiles
- Example values for multiple cloud providers
- Coordinated upgrades

## Quick Start

```bash
# Install complete stack (Lite profile)
helm install connektn ./connektn-stack \
  --create-namespace \
  --namespace connektn \
  --set connektn-agent.secrets.stripeApiKey='sk_test_...' \
  --set connektn-agent.secrets.stripeWebhookSecret='whsec_...' \
  --set connektn-agent.secrets.tenantSalt='your-random-salt'
```

## Documentation

- [Deployment Guide](DEPLOYMENT.md) - Comprehensive deployment documentation
- [Minikube Example](../examples/minikube/) - Local development setup
- [EKS Example](../examples/eks/) - AWS production deployment
- [GKE Example](../examples/gke/) - GCP production deployment (coming soon)
- [AKS Example](../examples/aks/) - Azure production deployment (coming soon)

## Deployment Profiles

### Lite Profile (Default)

Best for development, testing, and small-scale production deployments.

```bash
helm install connektn ./connektn-stack --namespace connektn
```

**Characteristics:**
- Single agent pod with embedded WAL queue
- 10GB PVC for persistent storage
- 2 gateway replicas
- No external dependencies

### HA Profile

Best for high-throughput production deployments.

```bash
helm install connektn ./connektn-stack \
  --namespace connektn \
  --values ./connektn-stack/values-ha.yaml
```

**Characteristics:**
- 3+ agent replicas with HPA
- External Kafka or RabbitMQ
- 3+ gateway replicas with HPA
- Consumer group coordination

## Prerequisites

| Profile | Kubernetes | Helm | PV Provisioner | Message Queue |
|---------|-----------|------|----------------|---------------|
| Lite    | 1.19+     | 3.0+ | ✅ Required    | ❌ Not needed |
| HA      | 1.19+     | 3.0+ | ❌ Not needed  | ✅ Kafka/RabbitMQ |

## Architecture

### Lite Profile

```
┌──────────────────┐
│   LoadBalancer   │
└────────┬─────────┘
         │
┌────────▼─────────┐
│     Gateway      │
│   (2 replicas)   │
└────────┬─────────┘
         │
┌────────▼─────────┐
│      Agent       │
│  (1 replica +    │
│   embedded WAL)  │
└────────┬─────────┘
         │
┌────────▼─────────┐
│   Connektn API   │
└──────────────────┘
```

### HA Profile

```
┌──────────────────┐
│   LoadBalancer   │
└────────┬─────────┘
         │
┌────────▼─────────┐
│     Gateway      │
│  (3+ replicas    │
│     + HPA)       │
└────────┬─────────┘
         │
┌────────▼─────────┐
│   Agent Service  │
└────────┬─────────┘
         │
    ┌────┴────┬────────┐
    │         │        │
┌───▼───┐ ┌──▼───┐ ┌──▼───┐
│Agent 1│ │Agent2│ │Agent3│
│(HPA)  │ │(HPA) │ │(HPA) │
└───┬───┘ └──┬───┘ └──┬───┘
    │        │        │
    └────┬───┴────────┘
         │
┌────────▼─────────┐
│  Kafka/RabbitMQ  │
│  (consumer grp)  │
└────────┬─────────┘
         │
┌────────▼─────────┐
│   Connektn API   │
└──────────────────┘
```

## Configuration

All charts support extensive configuration via `values.yaml`. Key areas:

### Secrets

```yaml
# Option 1: Direct values (dev only)
secrets:
  stripeApiKey: "sk_test_..."
  stripeWebhookSecret: "whsec_..."
  tenantSalt: "random-salt-32-chars"

# Option 2: External secret (recommended)
secrets:
  existingSecret: "connektn-secrets"
```

### Privacy

```yaml
config:
  privacy:
    mode: "strict"        # Always use strict in production
    idMode: "strict"      # Never use passthrough
```

### Export

```yaml
config:
  export:
    mode: "http"          # http | file | both
    http:
      baseUrl: "https://api.connektn.io"
      batchSize: 100
      flushEvery: 5s
```

### Resources

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

## CI/CD

A GitHub Actions workflow is provided for automated testing:

- **Lint**: All charts are linted with `helm lint` and `ct lint`
- **Template**: Charts are templated for both Lite and HA profiles
- **Install**: Charts are deployed to a kind cluster
- **Test**: Deployments are verified with health checks
- **Package**: Charts are packaged and versioned on merge to main
- **Security**: Charts are scanned with Checkov and kubesec

Workflow: [`.github/workflows/helm.yml`](../.github/workflows/helm.yml)

## Examples

Platform-specific examples are provided:

- **[Minikube](../examples/minikube/)** - Local development on Minikube
- **[AWS EKS](../examples/eks/)** - Production deployment on EKS with MSK
- **[GCP GKE](../examples/gke/)** - Coming soon
- **[Azure AKS](../examples/aks/)** - Coming soon

## Security

All charts follow security best practices:

- ✅ Non-root containers (UID 65532)
- ✅ Read-only root filesystem
- ✅ No privilege escalation
- ✅ Drop all capabilities
- ✅ Seccomp profile enabled
- ✅ Network policies (configurable)
- ✅ Pod security standards compliant

## Monitoring

Integration with Prometheus is built-in:

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
  labels:
    prometheus: kube-prometheus
```

Key metrics (implemented in Phase 2, Story 3):
- Queue depth and DLQ size
- Match counts and confidence scores
- HTTP request metrics
- Retry and failure rates

## Upgrading

```bash
# Rolling update
helm upgrade connektn ./connektn-stack \
  --namespace connektn \
  --reuse-values

# With new values
helm upgrade connektn ./connektn-stack \
  --namespace connektn \
  --values new-values.yaml
```

## Uninstalling

```bash
# Uninstall release
helm uninstall connektn -n connektn

# Delete PVC (Lite profile only)
kubectl delete pvc -l app.kubernetes.io/instance=connektn -n connektn

# Delete namespace
kubectl delete namespace connektn
```

## Development

### Testing Charts Locally

```bash
# Lint all charts
helm lint charts/connektn-agent
helm lint charts/connektn-gateway
helm lint charts/connektn-stack

# Template and inspect manifests
helm template test charts/connektn-stack \
  --set connektn-agent.secrets.stripeApiKey=test \
  --set connektn-agent.secrets.stripeWebhookSecret=test \
  --set connektn-agent.secrets.tenantSalt=test-salt-32-chars

# Dry-run install
helm install connektn charts/connektn-stack \
  --dry-run --debug \
  --set connektn-agent.secrets.stripeApiKey=test
```

### Building Dependencies

```bash
cd charts/connektn-stack
helm dependency update
```

## Troubleshooting

### Chart fails to install

```bash
# Check Helm release status
helm status connektn -n connektn

# View release history
helm history connektn -n connektn

# Get rendered manifests
helm get manifest connektn -n connektn
```

### Template errors

```bash
# Debug template rendering
helm template test charts/connektn-stack --debug
```

### Values not applied

```bash
# View effective values
helm get values connektn -n connektn

# View all values (including defaults)
helm get values connektn -n connektn --all
```

## Support

- **Documentation**: [Deployment Guide](DEPLOYMENT.md)
- **Examples**: [`examples/`](../examples/)
- **GitHub**: https://github.com/connektn/linker-agent
- **Issues**: https://github.com/connektn/linker-agent/issues

## License

Apache 2.0
