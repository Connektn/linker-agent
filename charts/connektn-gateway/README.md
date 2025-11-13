# Connektn Gateway Helm Chart

This Helm chart deploys the Connektn Gateway - an NGINX or Envoy-based gateway adapter that provides mTLS, rate limiting, and routing for Connektn Agent communication.

## Overview

The Connektn Gateway acts as a secure ingress point for Stripe webhooks and other external traffic destined for the Connektn Agent. It provides:

- TLS/mTLS termination
- Rate limiting
- Request routing
- Access logging
- Health check endpoints

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- Connektn Agent deployed (or deployed together via umbrella chart)

## Installation

### Standalone Installation

```bash
helm install connektn-gateway ./charts/connektn-gateway \
  --namespace connektn \
  --set config.upstream.serviceName=connektn-agent
```

### With TLS

```bash
# Create TLS secret
kubectl create secret tls connektn-gateway-tls \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key \
  -n connektn

# Install with TLS
helm install connektn-gateway ./charts/connektn-gateway \
  --namespace connektn \
  --set config.tls.enabled=true \
  --set config.tls.existingSecret=connektn-gateway-tls
```

## Configuration

### Key Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `type` | Gateway type: `nginx` or `envoy` | `nginx` |
| `replicaCount` | Number of replicas | `2` |
| `service.type` | Service type | `LoadBalancer` |
| `config.upstream.serviceName` | Agent service name | `connektn-agent` |
| `config.tls.enabled` | Enable TLS | `true` |
| `config.tls.clientCertRequired` | Require client certs (mTLS) | `false` |
| `config.rateLimit.enabled` | Enable rate limiting | `true` |
| `config.rateLimit.requestsPerSecond` | Rate limit per IP | `10` |

### Routing Configuration

The gateway routes the following paths to the agent:

- `/webhooks/stripe` - Stripe webhook endpoint (rate limited)
- `/healthz` - Health check (no rate limit)
- `/readyz` - Readiness check (no rate limit)
- `/metrics` - Prometheus metrics

All other paths return 404.

### Rate Limiting

Rate limiting is applied per source IP address:

```yaml
config:
  rateLimit:
    enabled: true
    requestsPerSecond: 10
    burst: 20
```

This allows 10 requests per second with a burst of 20 additional requests.

## Examples

### Production Setup with LoadBalancer

```bash
helm install connektn-gateway ./charts/connektn-gateway \
  --namespace connektn \
  --set replicaCount=3 \
  --set autoscaling.enabled=true \
  --set service.type=LoadBalancer \
  --set config.tls.enabled=true \
  --set config.rateLimit.requestsPerSecond=50
```

### Internal Gateway (ClusterIP)

```bash
helm install connektn-gateway ./charts/connektn-gateway \
  --namespace connektn \
  --set service.type=ClusterIP \
  --set config.tls.enabled=false
```

## Monitoring

The gateway exposes NGINX metrics and can be monitored via:

- Access logs (JSON format)
- NGINX stub_status module
- Prometheus exporter (via sidecar, future enhancement)

## Troubleshooting

### Check gateway status

```bash
kubectl get pods -l app.kubernetes.io/name=connektn-gateway -n connektn
```

### View logs

```bash
kubectl logs -f deployment/connektn-gateway -n connektn
```

### Test gateway endpoints

```bash
# Get LoadBalancer IP
export GATEWAY_IP=$(kubectl get svc connektn-gateway -n connektn -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Test health endpoint
curl -k https://$GATEWAY_IP/healthz
```

## Security

The gateway enforces secure defaults:

- Runs as non-root user
- Read-only root filesystem
- TLS 1.2+ only
- Strong cipher suites
- Rate limiting enabled by default

## Upgrading

```bash
helm upgrade connektn-gateway ./charts/connektn-gateway \
  --namespace connektn \
  --reuse-values
```

## Uninstalling

```bash
helm uninstall connektn-gateway -n connektn
```

## Support

- Documentation: https://docs.connektn.io
- GitHub: https://github.com/connektn/linker-agent
