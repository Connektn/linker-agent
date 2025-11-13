# Connektn Agent Helm Chart

This Helm chart deploys the Connektn Linker Agent - a privacy-preserving billing data reconciliation agent that runs on-premise.

## Overview

The Connektn Linker Agent reconciles usage data with Stripe billing data using synthetic identifiers and privacy-preserving matchers. It never stores or transmits PII, only anonymized graphs and cryptographic proofs of linkage.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- PV provisioner support in the underlying infrastructure (for Lite profile)
- Stripe API key and webhook signing secret

## Installation

### Quick Start (Lite Profile)

```bash
# Create namespace
kubectl create namespace connektn

# Create secrets
kubectl create secret generic connektn-agent-secrets \
  --from-literal=STRIPE_API_KEY='sk_test_...' \
  --from-literal=STRIPE_WEBHOOK_SECRET='whsec_...' \
  --from-literal=TENANT_SALT='your-random-salt' \
  -n connektn

# Install chart
helm install connektn-agent ./charts/connektn-agent \
  --namespace connektn \
  --set secrets.existingSecret=connektn-agent-secrets
```

### HA Profile with Kafka

```bash
helm install connektn-agent ./charts/connektn-agent \
  --namespace connektn \
  --set profile=ha \
  --set replicaCount=3 \
  --set autoscaling.enabled=true \
  --set queue.type=kafka \
  --set queue.kafka.brokers={kafka-1:9092,kafka-2:9092,kafka-3:9092} \
  --set secrets.existingSecret=connektn-agent-secrets
```

## Configuration

### Deployment Profiles

The chart supports two deployment profiles:

#### Lite Profile (Default)
- Single-agent pod with embedded WAL queue
- PVC attached for persistent storage
- Ideal for small to medium workloads
- Suitable for edge deployments

#### HA Profile
- Multi-replica deployment with external message queue (Kafka or RabbitMQ)
- Consumer groups for distributed processing
- Horizontal pod autoscaling support
- Ideal for high-throughput production workloads

### Key Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `profile` | Deployment profile: `lite` or `ha` | `lite` |
| `mode` | Operation mode: `webhook`, `matcher`, or `export-billing` | `webhook` |
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `ghcr.io/connektn/linker-agent` |
| `image.tag` | Image tag | Chart appVersion |
| `persistence.enabled` | Enable persistent storage (Lite profile) | `true` |
| `persistence.size` | PVC size | `10Gi` |
| `persistence.storageClass` | Storage class | `""` |
| `autoscaling.enabled` | Enable HPA (HA profile) | `false` |
| `queue.type` | Queue backend: `embedded`, `kafka`, or `rabbitmq` | `embedded` |
| `secrets.existingSecret` | Use existing secret instead of creating one | `""` |

### Secrets Configuration

The following secrets are required:

- `STRIPE_API_KEY`: Your Stripe API key
- `STRIPE_WEBHOOK_SECRET`: Stripe webhook signing secret
- `TENANT_SALT`: Random salt for synthetic ID generation
- `CONNEKTN_TENANT_KEY`: (Optional) API key for Connektn Cloud

You can either:
1. Create a Kubernetes secret and reference it via `secrets.existingSecret`
2. Pass secrets directly via values (NOT recommended for production)

Example secret creation:

```bash
kubectl create secret generic connektn-agent-secrets \
  --from-literal=STRIPE_API_KEY='sk_test_...' \
  --from-literal=STRIPE_WEBHOOK_SECRET='whsec_...' \
  --from-literal=TENANT_SALT='your-random-salt-min-32-chars' \
  --from-literal=CONNEKTN_TENANT_KEY='your-tenant-key' \
  -n connektn
```

### Privacy Configuration

The agent supports two privacy modes:

- `strict`: All IDs are synthetic (HMAC-based). No raw platform IDs are exposed.
- `standard`: Legacy mode (not recommended).

And two ID modes:

- `strict`: All IDs are hashed with tenant salt.
- `passthrough`: Raw platform IDs are used (requires `allowPassthroughExports: true`).

**Recommendation**: Always use `privacy.mode: strict` and `privacy.idMode: strict` for production.

### Export Configuration

The agent can export data to:

- HTTP endpoint (Connektn Cloud API)
- Local files (for debugging)
- Both

Configure via `config.export.mode`: `http`, `file`, or `both`.

### Health Checks

The agent exposes the following endpoints:

- `/healthz`: Liveness probe (returns 200 if server is running)
- `/readyz`: Readiness probe (returns 200 if ready to process webhooks)
- `/metrics`: Prometheus metrics endpoint (placeholder in Phase 1)

## Examples

### Minikube

```bash
# Start minikube
minikube start

# Install with port-forward access
helm install connektn-agent ./charts/connektn-agent \
  --set secrets.stripeApiKey='sk_test_...' \
  --set secrets.stripeWebhookSecret='whsec_...' \
  --set secrets.tenantSalt='my-random-salt-32-chars-min'

# Forward port
kubectl port-forward svc/connektn-agent 8080:8080

# Test health endpoint
curl http://localhost:8080/healthz
```

### Production (EKS/GKE/AKS)

```bash
# Install with ingress and external secrets
helm install connektn-agent ./charts/connektn-agent \
  --namespace connektn \
  --set profile=ha \
  --set replicaCount=3 \
  --set autoscaling.enabled=true \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.hosts[0].host=linker.example.com \
  --set ingress.hosts[0].paths[0].path=/ \
  --set ingress.hosts[0].paths[0].pathType=Prefix \
  --set secrets.existingSecret=connektn-agent-secrets \
  --set serviceMonitor.enabled=true
```

## Monitoring

### Prometheus Integration

Enable Prometheus monitoring:

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
  labels:
    prometheus: kube-prometheus
```

This requires the Prometheus Operator to be installed in your cluster.

## Security

### Pod Security

The chart enforces secure defaults:

- Runs as non-root user (UID 65532)
- Read-only root filesystem
- Drops all capabilities
- Uses seccomp profile

### mTLS

Enable mTLS for inter-component communication:

```yaml
mtls:
  enabled: true
  certManager:
    enabled: true
    issuerRef:
      name: connektn-issuer
      kind: ClusterIssuer
```

## Troubleshooting

### Check pod status

```bash
kubectl get pods -n connektn
kubectl describe pod <pod-name> -n connektn
```

### View logs

```bash
kubectl logs -f deployment/connektn-agent -n connektn
```

### Test health endpoints

```bash
kubectl exec -it deployment/connektn-agent -n connektn -- wget -q -O- http://localhost:8080/healthz
kubectl exec -it deployment/connektn-agent -n connektn -- wget -q -O- http://localhost:8080/readyz
```

### Verify configuration

```bash
kubectl get configmap connektn-agent -n connektn -o yaml
```

## Upgrading

```bash
helm upgrade connektn-agent ./charts/connektn-agent \
  --namespace connektn \
  --reuse-values
```

## Uninstalling

```bash
helm uninstall connektn-agent -n connektn

# Optional: Delete PVC (Lite profile)
kubectl delete pvc connektn-agent -n connektn
```

## Values Reference

For a complete list of configuration options, see [values.yaml](values.yaml).

## Support

- Documentation: https://docs.connektn.io
- GitHub: https://github.com/connektn/linker-agent
- Issues: https://github.com/connektn/linker-agent/issues
