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

### 2. Build and Load Docker Image

Since the Connektn images aren't published to a registry yet, you need to build them locally and load them into Minikube:

```bash
# Point your shell to Minikube's Docker daemon
eval $(minikube docker-env)

# Build the image directly in Minikube's Docker (from repository root)
docker build -t ghcr.io/connektn/linker-agent:latest .

# Verify the image is available
docker images | grep linker-agent
```

**Important**: Keep this terminal session open, or re-run `eval $(minikube docker-env)` in any new terminal where you need to interact with Minikube's Docker.

### 3. Create Secrets

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

### 4. Install Connektn Stack

```bash
# From repository root
helm install connektn ./charts/connektn-stack \
  --namespace connektn \
  --values examples/minikube/values-lite.yaml \
  --set connektn-agent.secrets.existingSecret=connektn-secrets \
  --set connektn-agent.image.pullPolicy=Never
```

**Note**: The `--set connektn-agent.image.pullPolicy=Never` flag tells Kubernetes to use the local image and never try to pull from a registry.

### 5. Wait for Pods to be Ready

```bash
# Watch pods until they're running
kubectl get pods -n connektn -w

# Once both pods show RUNNING and READY 1/1, press Ctrl+C to exit watch mode
```

### 6. Access the Gateway

The recommended way to access the gateway on Minikube (macOS with Docker driver) is using `kubectl port-forward`:

```bash
# In a dedicated terminal, run:
kubectl port-forward svc/connektn-connektn-gateway 8080:8080 -n connektn

# Keep this terminal open while testing
```

Alternatively, you can use `minikube service` (requires keeping terminal open):

```bash
# This creates a tunnel and must stay running
minikube service connektn-connektn-gateway -n connektn
```

## Testing

### Test Health Endpoints

In a **new terminal** (while port-forward is running in another terminal):

```bash
# Set the gateway URL
export GATEWAY_URL=http://localhost:8080

# Test health
curl $GATEWAY_URL/healthz

# Test readiness
curl $GATEWAY_URL/readyz
```

Expected responses:
- `/healthz` should return `200 OK`
- `/readyz` should return `200 OK` (once agent is connected)

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

### ImagePullBackOff Error

If you see `ImagePullBackOff` on the agent pod:

```bash
# Make sure you built the image in Minikube's Docker
eval $(minikube docker-env)
docker images | grep linker-agent

# If the image is missing, rebuild it
docker build -t ghcr.io/connektn/linker-agent:latest .

# Then restart the pod
kubectl rollout restart deployment/connektn-connektn-agent -n connektn
```

### Pod not starting

```bash
kubectl describe pod -l app.kubernetes.io/name=connektn-agent -n connektn
kubectl logs -l app.kubernetes.io/name=connektn-agent -n connektn
```

### Gateway CrashLoopBackOff

The gateway depends on the agent being healthy. Check agent status first:

```bash
# Check if agent is running
kubectl get pods -n connektn

# Check agent logs
kubectl logs -l app.kubernetes.io/name=connektn-agent -n connektn

# Check gateway logs
kubectl logs -l app.kubernetes.io/name=connektn-gateway -n connektn
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

# Optional: Reset Docker environment to your local Docker
eval $(minikube docker-env -u)

# Stop Minikube
minikube stop
```

**Note**: Use `eval $(minikube docker-env -u)` to point your shell back to your local Docker daemon if you want to build images for non-Minikube use.

## Next Steps

- Configure HTTP export to Connektn Cloud
- Try HA profile with Kafka (requires more resources)
- Integrate with Stripe test mode webhooks
