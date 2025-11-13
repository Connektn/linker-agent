# Connektn on Minikube

This example demonstrates how to deploy Connektn on Minikube for local development and testing.

## Prerequisites

- [Minikube](https://minikube.sigs.k8s.io/docs/start/) installed
- [kubectl](https://kubernetes.io/docs/tasks/tools/) installed
- [Helm](https://helm.sh/docs/intro/install/) 3.0+ installed
- Stripe test API key and webhook signing secret

## Installation

### 1. Start Minikube

```bash
# Start with sufficient resources
minikube start --memory 4096 --cpus 2

# Enable metrics-server (optional, for HPA)
minikube addons enable metrics-server
```

### 2. Create Secrets

```bash
# Create namespace
kubectl create namespace connektn

# Create secrets
kubectl create secret generic connektn-secrets \
  --from-literal=STRIPE_API_KEY='sk_test_...' \
  --from-literal=STRIPE_WEBHOOK_SECRET='whsec_...' \
  --from-literal=TENANT_SALT='your-random-salt-at-least-32-chars' \
  -n connektn
```

### 3. Install Connektn Stack

```bash
# From repository root
helm install connektn ./charts/connektn-stack \
  --namespace connektn \
  --values examples/minikube/values-lite.yaml \
  --set connektn-agent.secrets.existingSecret=connektn-secrets
```

### 4. Access the Gateway

```bash
# Get the NodePort URL
minikube service connektn-connektn-gateway -n connektn --url

# Or use port-forward
kubectl port-forward svc/connektn-connektn-gateway 8080:8080 -n connektn
```

## Testing

### Test Health Endpoints

```bash
# Get the URL
export GATEWAY_URL=$(minikube service connektn-connektn-gateway -n connektn --url)

# Test health
curl $GATEWAY_URL/healthz

# Test readiness
curl $GATEWAY_URL/readyz
```

### Configure Stripe Webhook

For local testing with Stripe webhooks, use the Stripe CLI:

```bash
# Install Stripe CLI
# https://stripe.com/docs/stripe-cli

# Forward webhooks to your local gateway
stripe listen --forward-to $GATEWAY_URL/webhooks/stripe
```

### View Logs

```bash
# Agent logs
kubectl logs -f deployment/connektn-connektn-agent -n connektn

# Gateway logs
kubectl logs -f deployment/connektn-connektn-gateway -n connektn
```

### Check Exported Data

Since we're using file export mode, you can check the exported files:

```bash
# Exec into the agent pod
kubectl exec -it deployment/connektn-connektn-agent -n connektn -- sh

# List exported files
ls -lh /var/lib/connektn/wal/

# View edges
cat /var/lib/connektn/wal/link_edges.jsonl

# View billing data
cat /var/lib/connektn/wal/billing.jsonl
```

## Troubleshooting

### Pod not starting

```bash
kubectl describe pod -l app.kubernetes.io/name=connektn-agent -n connektn
kubectl logs -l app.kubernetes.io/name=connektn-agent -n connektn
```

### PVC not binding

```bash
kubectl get pvc -n connektn
kubectl describe pvc connektn-connektn-agent -n connektn

# Check storage class
kubectl get storageclass
```

### Gateway can't reach Agent

```bash
# Check services
kubectl get svc -n connektn

# Test connectivity from gateway to agent
kubectl exec -it deployment/connektn-connektn-gateway -n connektn -- \
  wget -O- http://connektn-stack-connektn-agent:8080/healthz
```

## Cleanup

```bash
# Uninstall Connektn
helm uninstall connektn -n connektn

# Delete PVC
kubectl delete pvc connektn-connektn-agent -n connektn

# Delete namespace
kubectl delete namespace connektn

# Stop Minikube
minikube stop
```

## Next Steps

- Configure HTTP export to Connektn Cloud
- Try HA profile with Kafka (requires more resources)
- Integrate with Stripe test mode webhooks
