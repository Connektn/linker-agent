# Connektn Kubernetes Deployment Guide

This guide covers deploying Connektn on Kubernetes using Helm charts in both Lite and HA profiles.

## Overview

Connektn provides three Helm charts:

1. **connektn-agent**: Core privacy-preserving reconciliation agent
2. **connektn-gateway**: NGINX/Envoy gateway for secure ingress
3. **connektn-stack**: Umbrella chart deploying both components

## Deployment Profiles

### Lite Profile

**Best for:**
- Development and testing
- Small to medium workloads (< 1000 events/hour)
- Edge deployments
- Single-node or small clusters

**Architecture:**
- Single agent pod (can be scaled manually)
- Embedded WAL queue with persistent storage
- 2 gateway replicas
- No external dependencies

**Resource Requirements:**
- **Minimum**: 2 CPU cores, 4 GB RAM
- **Recommended**: 4 CPU cores, 8 GB RAM
- **Storage**: 10 GB PVC for WAL

### HA Profile

**Best for:**
- Production environments
- High-throughput workloads (> 1000 events/hour)
- Multi-region deployments
- Mission-critical applications

**Architecture:**
- 3+ agent replicas with HPA
- External message queue (Kafka or RabbitMQ)
- 3+ gateway replicas with HPA
- Consumer groups for distributed processing

**Resource Requirements:**
- **Minimum**: 8 CPU cores, 16 GB RAM
- **Recommended**: 16+ CPU cores, 32+ GB RAM
- **External**: Kafka or RabbitMQ cluster

## Quick Start

### Install Lite Profile

```bash
# Create namespace and secrets
kubectl create namespace connektn
kubectl create secret generic connektn-secrets \
  --from-literal=STRIPE_API_KEY='sk_test_...' \
  --from-literal=STRIPE_WEBHOOK_SECRET='whsec_...' \
  --from-literal=TENANT_SALT='your-random-salt-32-chars' \
  -n connektn

# Install stack
helm install connektn ./connektn-stack \
  --namespace connektn \
  --set connektn-agent.secrets.existingSecret=connektn-secrets
```

### Install HA Profile

```bash
# Prerequisites: Kafka cluster deployed
helm install connektn ./connektn-stack \
  --namespace connektn \
  --values ./connektn-stack/values-ha.yaml \
  --set connektn-agent.secrets.existingSecret=connektn-secrets
```

## Detailed Configuration

### Agent Configuration

#### Privacy Settings

```yaml
connektn-agent:
  config:
    privacy:
      mode: "strict"        # Always use strict in production
      idMode: "strict"      # Never use passthrough in production
      allowPassthroughExports: false
```

#### Export Configuration

**HTTP Export (Recommended for Production):**

```yaml
connektn-agent:
  config:
    export:
      mode: "http"
      http:
        baseUrl: "https://api.connektn.io"
        maxRetries: 5
        batchSize: 100
        flushEvery: 3s
```

**File Export (For Development/Debugging):**

```yaml
connektn-agent:
  config:
    export:
      mode: "file"
      file:
        paths:
          edges: "/var/lib/connektn/wal/link_edges.jsonl"
          billing: "/var/lib/connektn/wal/billing.jsonl"
```

#### Matcher Configuration

```yaml
connektn-agent:
  config:
    matchers:
      recipe:
        name: "default"
        version: "v1"
        weights:
          deterministic_id: 1.0
          temporal_proximity: 0.5
          sku_overlap: 0.8
        threshold: 0.8
        temporalWindowSec: 3600
```

### Gateway Configuration

#### TLS Configuration

```yaml
connektn-gateway:
  config:
    tls:
      enabled: true
      clientCertRequired: false  # Set to true for mTLS
      certManager:
        enabled: true
        issuerRef:
          name: letsencrypt-prod
          kind: ClusterIssuer
```

#### Rate Limiting

```yaml
connektn-gateway:
  config:
    rateLimit:
      enabled: true
      requestsPerSecond: 100  # Adjust based on expected load
      burst: 200
```

### Queue Configuration (HA Profile)

#### Kafka

```yaml
connektn-agent:
  queue:
    type: kafka
    kafka:
      brokers:
        - kafka-0:9092
        - kafka-1:9092
        - kafka-2:9092
      topics:
        events: "connektn-events"
        dlq: "connektn-dlq"
      consumerGroup: "connektn-agents"
      sasl:
        enabled: true
        mechanism: "SCRAM-SHA-512"
      tls:
        enabled: true
```

#### RabbitMQ

```yaml
connektn-agent:
  queue:
    type: rabbitmq
    rabbitmq:
      host: "rabbitmq.default.svc.cluster.local"
      port: 5672
      queues:
        events: "connektn-events"
        dlq: "connektn-dlq"
      auth:
        existingSecret: "rabbitmq-credentials"
```

## Secrets Management

### Option 1: Kubernetes Secrets (Development)

```bash
kubectl create secret generic connektn-secrets \
  --from-literal=STRIPE_API_KEY='sk_test_...' \
  --from-literal=STRIPE_WEBHOOK_SECRET='whsec_...' \
  --from-literal=TENANT_SALT='...' \
  --from-literal=CONNEKTN_TENANT_KEY='...' \
  -n connektn
```

### Option 2: External Secrets Operator (Production)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: connektn-secrets
  namespace: connektn
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend  # or aws-secrets-manager, gcpsm, etc.
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
```

### Option 3: Sealed Secrets

```bash
# Install sealed-secrets controller
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.0/controller.yaml

# Create and seal secret
kubectl create secret generic connektn-secrets \
  --from-literal=STRIPE_API_KEY='...' \
  --dry-run=client -o yaml | \
  kubeseal -o yaml > sealed-secret.yaml

kubectl apply -f sealed-secret.yaml
```

## Resource Planning

### Lite Profile

| Component | CPU Request | CPU Limit | Memory Request | Memory Limit |
|-----------|-------------|-----------|----------------|--------------|
| Agent     | 100m        | 500m      | 128Mi          | 512Mi        |
| Gateway   | 50m         | 200m      | 64Mi           | 256Mi        |

**Per-pod totals**: 150m CPU, 192Mi RAM
**Total (3 pods)**: 450m CPU, 576Mi RAM

### HA Profile

| Component | CPU Request | CPU Limit | Memory Request | Memory Limit |
|-----------|-------------|-----------|----------------|--------------|
| Agent     | 250m        | 1000m     | 256Mi          | 1Gi          |
| Gateway   | 100m        | 500m      | 128Mi          | 512Mi        |

**Per-pod totals**: 350m CPU, 384Mi RAM
**Total (6 pods min)**: 2100m CPU, 2.3Gi RAM

## Storage Requirements

### Lite Profile

```yaml
persistence:
  enabled: true
  size: 10Gi  # Start with 10Gi
  storageClass: ""  # Use default
```

**Growth estimate**: ~100 MB per 10,000 events

### HA Profile

No persistent storage required. Kafka handles durability.

## Monitoring and Observability

### Prometheus Integration

```yaml
connektn-agent:
  serviceMonitor:
    enabled: true
    interval: 30s
    labels:
      prometheus: kube-prometheus
```

### Key Metrics (Phase 2, Story 3)

- `linker_matches_total`: Total matches created
- `linker_confidence_avg`: Average match confidence
- `linker_queue_depth`: Current queue depth
- `linker_dlq_size`: Dead letter queue size
- `linker_http_requests_total`: HTTP request counter

### Log Aggregation

Logs are written to stdout in JSON format. Configure your log aggregation:

**FluentD:**
```yaml
<source>
  @type tail
  path /var/log/containers/connektn-*.log
  pos_file /var/log/connektn.pos
  tag kubernetes.*
  format json
</source>
```

**Promtail (Loki):**
```yaml
scrape_configs:
  - job_name: kubernetes-pods
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app_kubernetes_io_name]
        regex: connektn-agent
        action: keep
```

## Upgrading

### Rolling Updates

```bash
# Update image version
helm upgrade connektn ./connektn-stack \
  --namespace connektn \
  --set connektn-agent.image.tag=v0.2.0 \
  --reuse-values

# Monitor rollout
kubectl rollout status deployment/connektn-connektn-agent -n connektn
```

### Blue-Green Deployment

```bash
# Install new version with different release name
helm install connektn-v2 ./connektn-stack \
  --namespace connektn \
  --set connektn-agent.image.tag=v0.2.0

# Test new version
# Switch traffic (update DNS/LB)
# Uninstall old version
helm uninstall connektn -n connektn
```

### Migrate Lite to HA

```bash
# 1. Backup WAL data
kubectl cp connektn/connektn-agent-xxx:/var/lib/connektn/wal ./wal-backup

# 2. Deploy Kafka
helm install kafka bitnami/kafka -n connektn

# 3. Upgrade to HA profile
helm upgrade connektn ./connektn-stack \
  --namespace connektn \
  --values ./connektn-stack/values-ha.yaml \
  --reuse-values
```

## High Availability Considerations

### Pod Disruption Budgets

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: connektn-agent-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: connektn-agent
```

### Topology Spread Constraints

```yaml
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: connektn-agent
```

## Disaster Recovery

### Backup Strategy (Lite Profile)

```bash
# Backup PVC
kubectl get pvc connektn-connektn-agent -n connektn -o yaml > pvc-backup.yaml

# Snapshot volume (cloud-specific)
# AWS EBS
aws ec2 create-snapshot --volume-id vol-xxx --description "Connektn WAL backup"

# GCP PD
gcloud compute disks snapshot connektn-wal --zone us-central1-a
```

### Restore Procedure

```bash
# 1. Create PVC from snapshot
kubectl apply -f pvc-from-snapshot.yaml

# 2. Install with existing PVC
helm install connektn ./connektn-stack \
  --set connektn-agent.persistence.existingClaim=restored-pvc
```

## Security Hardening

### Network Policies

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: connektn-agent
  namespace: connektn
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: connektn-agent
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
      - podSelector:
          matchLabels:
            app.kubernetes.io/name: connektn-gateway
      ports:
      - protocol: TCP
        port: 8080
  egress:
    - to:
      - namespaceSelector: {}
      ports:
      - protocol: TCP
        port: 443  # Connektn Cloud API
    - to:
      - podSelector:
          matchLabels:
            app: kafka
      ports:
      - protocol: TCP
        port: 9092
```

### Pod Security Standards

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: connektn
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

## Troubleshooting

### Common Issues

#### Agent pods not starting

```bash
# Check pod events
kubectl describe pod -l app.kubernetes.io/name=connektn-agent -n connektn

# Check logs
kubectl logs -l app.kubernetes.io/name=connektn-agent -n connektn --tail=100

# Common causes:
# - Missing secrets
# - Invalid Stripe credentials
# - PVC not binding (Lite profile)
# - Kafka connectivity issues (HA profile)
```

#### Gateway can't reach Agent

```bash
# Test connectivity
kubectl exec -it deployment/connektn-connektn-gateway -n connektn -- \
  wget -O- http://connektn-stack-connektn-agent:8080/healthz

# Check service
kubectl get svc connektn-stack-connektn-agent -n connektn
kubectl get endpoints connektn-stack-connektn-agent -n connektn
```

#### PVC not binding (Lite profile)

```bash
# Check PVC status
kubectl get pvc -n connektn
kubectl describe pvc connektn-connektn-agent -n connektn

# Check storage class
kubectl get storageclass

# Solutions:
# - Specify valid storage class
# - Ensure PV provisioner is installed
# - Check node capacity
```

#### High memory usage

```bash
# Check memory usage
kubectl top pods -n connektn

# Solutions:
# - Reduce batch size in export config
# - Reduce replica count if over-provisioned
# - Increase memory limits if legitimate usage
```

### Debug Mode

```bash
# Port-forward to agent
kubectl port-forward deployment/connektn-connektn-agent 8080:8080 -n connektn

# Test health endpoints
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/metrics
```

## Performance Tuning

### Agent Tuning

```yaml
connektn-agent:
  config:
    sources:
      stripe:
        maxRequestsPerSecond: 20  # Increase for higher throughput
    export:
      http:
        batchSize: 200           # Larger batches = fewer requests
        flushEvery: 5s           # Adjust based on latency requirements
```

### Gateway Tuning

```yaml
connektn-gateway:
  nginx:
    clientMaxBodySize: 2m      # Increase for larger webhook payloads
    proxyConnectTimeout: 120s  # Increase for slow upstream
    proxyReadTimeout: 120s
```

### Kafka Tuning (HA Profile)

```yaml
queue:
  kafka:
    topics:
      events:
        partitions: 10         # More partitions = more parallelism
        replicationFactor: 3
```

## Examples

See `examples/` directory for platform-specific deployments:

- **Minikube**: Local development setup
- **EKS**: AWS production deployment
- **GKE**: Google Cloud production deployment
- **AKS**: Azure production deployment

## Support

- **Documentation**: https://docs.connektn.io
- **GitHub**: https://github.com/connektn/linker-agent
- **Issues**: https://github.com/connektn/linker-agent/issues
- **Discord**: https://discord.gg/connektn

## License

Apache 2.0
