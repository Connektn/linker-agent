# Connektn Stack Helm Chart

This is an umbrella Helm chart that deploys the complete Connektn stack, including:

- **Connektn Agent**: Privacy-preserving billing reconciliation agent
- **Connektn Gateway**: NGINX/Envoy gateway for secure ingress

## Quick Start

### Lite Profile (Single Node / Edge)

Perfect for small to medium workloads, development, and edge deployments.

```bash
# Create namespace
kubectl create namespace connektn

# Create secrets
kubectl create secret generic connektn-secrets \
  --from-literal=STRIPE_API_KEY='sk_test_...' \
  --from-literal=STRIPE_WEBHOOK_SECRET='whsec_...' \
  --from-literal=TENANT_SALT='your-random-salt-at-least-32-chars' \
  -n connektn

# Install stack
helm install connektn ./charts/connektn-stack \
  --namespace connektn \
  --set connektn-agent.secrets.existingSecret=connektn-secrets \
  --create-namespace
```

### HA Profile (Production)

Optimized for high-throughput production workloads with Kafka backend.

```bash
# Prerequisites: Kafka cluster must be deployed
# Example: helm install kafka bitnami/kafka -n connektn

# Install stack with HA profile
helm install connektn ./charts/connektn-stack \
  --namespace connektn \
  --values charts/connektn-stack/values-ha.yaml \
  --set connektn-agent.secrets.existingSecret=connektn-secrets \
  --create-namespace
```

## Architecture

### Lite Profile

```
Internet
    │
    ▼
Gateway (LoadBalancer)
    │ (TLS termination, rate limiting)
    ▼
Agent (1 replica)
    │ (Embedded WAL queue + PVC)
    ▼
Connektn Cloud API
```

### HA Profile

```
Internet
    │
    ▼
Gateway (3+ replicas, autoscaling)
    │ (TLS termination, rate limiting)
    ▼
Agent (3+ replicas, autoscaling)
    │ (Consumer groups)
    ▼
Kafka/RabbitMQ
    │
    ▼
Connektn Cloud API
```

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- For Lite profile: PV provisioner
- For HA profile: Kafka or RabbitMQ cluster

## Configuration Profiles

### Lite Profile (Default)

| Component | Configuration |
|-----------|--------------|
| Agent | 1 replica, embedded WAL queue, PVC storage |
| Gateway | 2 replicas, no autoscaling |
| Queue | Embedded (file-based WAL) |
| Suitable for | Dev, test, small production, edge |

### HA Profile

| Component | Configuration |
|-----------|--------------|
| Agent | 3+ replicas, HPA enabled, Kafka/RabbitMQ |
| Gateway | 3+ replicas, HPA enabled |
| Queue | Kafka or RabbitMQ with consumer groups |
| Suitable for | Large-scale production, high throughput |

## Installation Examples

### Minikube (Development)

```bash
minikube start --memory 4096 --cpus 2

helm install connektn ./charts/connektn-stack \
  --set connektn-agent.secrets.stripeApiKey='sk_test_...' \
  --set connektn-agent.secrets.stripeWebhookSecret='whsec_...' \
  --set connektn-agent.secrets.tenantSalt='my-dev-salt' \
  --set connektn-gateway.service.type=NodePort

# Get webhook URL
minikube service connektn-connektn-gateway --url
```

### AWS EKS (Production HA)

```bash
# Create secrets using AWS Secrets Manager + External Secrets Operator
kubectl apply -f - <<EOF
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: connektn-secrets
  namespace: connektn
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: connektn-agent-secrets
  data:
    - secretKey: STRIPE_API_KEY
      remoteRef:
        key: connektn/stripe-api-key
    - secretKey: STRIPE_WEBHOOK_SECRET
      remoteRef:
        key: connektn/stripe-webhook-secret
    - secretKey: TENANT_SALT
      remoteRef:
        key: connektn/tenant-salt
    - secretKey: CONNEKTN_TENANT_KEY
      remoteRef:
        key: connektn/tenant-key
EOF

# Install with HA profile
helm install connektn ./charts/connektn-stack \
  --namespace connektn \
  --values charts/connektn-stack/values-ha.yaml \
  --set connektn-agent.secrets.existingSecret=connektn-agent-secrets \
  --set connektn-gateway.service.annotations."service\.beta\.kubernetes\.io/aws-load-balancer-type"=nlb \
  --create-namespace
```

### GKE (Production HA)

```bash
# Install with GKE-specific annotations
helm install connektn ./charts/connektn-stack \
  --namespace connektn \
  --values charts/connektn-stack/values-ha.yaml \
  --set connektn-agent.secrets.existingSecret=connektn-agent-secrets \
  --set connektn-gateway.service.annotations."cloud\.google\.com/load-balancer-type"=Internal \
  --create-namespace
```

## Component Configuration

### Agent Configuration

The agent can be configured via `connektn-agent.*` values:

```yaml
connektn-agent:
  profile: lite  # or "ha"
  mode: webhook  # or "matcher" or "export-billing"
  replicaCount: 1

  config:
    privacy:
      mode: "strict"
      idMode: "strict"
    export:
      mode: "http"
      http:
        baseUrl: "https://api.connektn.io"
```

### Gateway Configuration

The gateway can be configured via `connektn-gateway.*` values:

```yaml
connektn-gateway:
  type: nginx  # or "envoy"
  replicaCount: 2

  config:
    tls:
      enabled: true
    rateLimit:
      enabled: true
      requestsPerSecond: 10
```

## Secrets Management

### Option 1: Kubernetes Secret (Development)

```bash
kubectl create secret generic connektn-secrets \
  --from-literal=STRIPE_API_KEY='...' \
  --from-literal=STRIPE_WEBHOOK_SECRET='...' \
  --from-literal=TENANT_SALT='...' \
  -n connektn
```

### Option 2: External Secrets Operator (Recommended for Production)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: connektn-secrets
spec:
  secretStoreRef:
    name: aws-secrets-manager  # or vault, gcpsm, etc.
  target:
    name: connektn-agent-secrets
  data:
    - secretKey: STRIPE_API_KEY
      remoteRef:
        key: connektn/stripe-api-key
```

### Option 3: Sealed Secrets

```bash
# Create secret and seal it
kubectl create secret generic connektn-secrets \
  --from-literal=STRIPE_API_KEY='...' \
  --dry-run=client -o yaml | \
  kubeseal -o yaml > sealed-secret.yaml

kubectl apply -f sealed-secret.yaml
```

## Monitoring

### Prometheus Integration

Enable Prometheus monitoring:

```yaml
connektn-agent:
  serviceMonitor:
    enabled: true
    labels:
      prometheus: kube-prometheus
```

### Grafana Dashboards

Coming in Phase 2, Story 3.

## Upgrading

### Helm Upgrade

```bash
helm upgrade connektn ./charts/connektn-stack \
  --namespace connektn \
  --reuse-values
```

### Rolling Updates

The stack supports zero-downtime rolling updates:

```bash
# Update agent image
helm upgrade connektn ./charts/connektn-stack \
  --namespace connektn \
  --set connektn-agent.image.tag=v0.2.0 \
  --reuse-values
```

### Migrate from Lite to HA

```bash
# Backup WAL data first
kubectl cp connektn/connektn-agent-xxx:/var/lib/connektn/wal ./wal-backup

# Deploy Kafka
helm install kafka bitnami/kafka -n connektn

# Upgrade to HA profile
helm upgrade connektn ./charts/connektn-stack \
  --namespace connektn \
  --values charts/connektn-stack/values-ha.yaml \
  --reuse-values
```

## Troubleshooting

### Check component status

```bash
kubectl get pods -n connektn
kubectl get svc -n connektn
```

### View logs

```bash
# Agent logs
kubectl logs -f deployment/connektn-connektn-agent -n connektn

# Gateway logs
kubectl logs -f deployment/connektn-connektn-gateway -n connektn
```

### Test webhook endpoint

```bash
# Get gateway URL
export GATEWAY_URL=$(kubectl get svc connektn-connektn-gateway -n connektn -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Test health
curl -k https://$GATEWAY_URL/healthz

# Configure Stripe webhook endpoint
# https://$GATEWAY_URL/webhooks/stripe
```

### Common Issues

#### Gateway can't reach Agent

Check service names and namespace:

```bash
kubectl get svc -n connektn | grep agent
kubectl describe svc connektn-connektn-agent -n connektn
```

#### PVC not binding (Lite profile)

Check storage class:

```bash
kubectl get pvc -n connektn
kubectl describe pvc connektn-connektn-agent -n connektn

# Set specific storage class
helm upgrade connektn ./charts/connektn-stack \
  --set connektn-agent.persistence.storageClass=gp2 \
  --reuse-values
```

## Uninstalling

```bash
# Uninstall stack
helm uninstall connektn -n connektn

# Optional: Delete PVC (Lite profile)
kubectl delete pvc -l app.kubernetes.io/instance=connektn -n connektn

# Delete namespace
kubectl delete namespace connektn
```

## Values Reference

For detailed configuration options, see:

- [Agent values](../connektn-agent/values.yaml)
- [Gateway values](../connektn-gateway/values.yaml)

## Support

- Documentation: https://docs.connektn.io
- GitHub: https://github.com/connektn/linker-agent
- Issues: https://github.com/connektn/linker-agent/issues

## License

Apache 2.0
