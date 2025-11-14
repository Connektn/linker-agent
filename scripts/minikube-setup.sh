#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

# -------------------------------------------------------------------
# Connektn Minikube Setup Script
# -------------------------------------------------------------------
# Automates the deployment of Connektn stack on Minikube for local
# development and testing.
# -------------------------------------------------------------------

NAMESPACE="connektn"
RELEASE_NAME="connektn"
IMAGE_NAME="ghcr.io/connektn/linker-agent:latest"
MEMORY="${MINIKUBE_MEMORY:-4096}"
CPUS="${MINIKUBE_CPUS:-2}"

# -------------------------------------------------------------------
# Helpers
# -------------------------------------------------------------------

log() { printf "\n[+] %s\n" "$*"; }
warn() { printf "\n[!] %s\n" "$*" >&2; }
error() { printf "\n[✗] %b\n" "$*" >&2; exit 1; }

# -------------------------------------------------------------------
# Pre-flight checks
# -------------------------------------------------------------------

log "🔍 Checking environment..."

# Check required tools
for cmd in minikube kubectl helm docker; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        error "$cmd not found. Please install it first."
    fi
done

# Check required environment variables
if [[ -z "${STRIPE_API_KEY:-}" ]]; then
    error "STRIPE_API_KEY not set. Please export it first:\n   export STRIPE_API_KEY=sk_test_..."
fi

if [[ "${STRIPE_API_KEY}" != sk_test_* ]]; then
    error "STRIPE_API_KEY must be a TEST key (sk_test_...).\n   Current key starts with: ${STRIPE_API_KEY:0:7}"
fi

if [[ -z "${STRIPE_WEBHOOK_SECRET:-}" ]]; then
    error "STRIPE_WEBHOOK_SECRET not set. Please export it first:\n   export STRIPE_WEBHOOK_SECRET=whsec_..."
fi

if [[ -z "${TENANT_SALT:-}" ]]; then
    error "TENANT_SALT not set. Please export it first:\n   export TENANT_SALT='your-random-salt-at-least-32-chars'"
fi

if [[ ${#TENANT_SALT} -lt 32 ]]; then
    error "TENANT_SALT must be at least 32 characters long.\n   Current length: ${#TENANT_SALT}"
fi

if [[ -z "${ORGANIZATION_ID:-}" ]]; then
    error "ORGANIZATION_ID not set. Please export it first:\n   export ORGANIZATION_ID='your-org-id'"
fi

if [[ -z "${HEARTBEAT_SECRET:-}" ]]; then
    error "HEARTBEAT_SECRET not set. Please export it first:\n   export HEARTBEAT_SECRET='your-heartbeat-secret'"
fi

if [[ -z "${CONTROL_SECRET:-}" ]]; then
    error "CONTROL_SECRET not set. Please export it first:\n   export CONTROL_SECRET='your-control-secret'"
fi

log "✅ Environment validated"

# -------------------------------------------------------------------
# Start Minikube
# -------------------------------------------------------------------

if minikube status >/dev/null 2>&1; then
    log "⚡ Minikube is already running"
else
    log "🚀 Starting Minikube (${MEMORY}MB RAM, ${CPUS} CPUs)..."
    minikube start --memory "$MEMORY" --cpus "$CPUS"

    log "📊 Enabling metrics-server addon..."
    minikube addons enable metrics-server
fi

# -------------------------------------------------------------------
# Build and load Docker image
# -------------------------------------------------------------------

log "🐳 Building Docker image in Minikube's Docker environment..."

# Export Minikube's Docker environment variables
eval "$(minikube docker-env)"

log "Docker host: ${DOCKER_HOST}"

# Build image (from repository root)
if ! docker build -t "$IMAGE_NAME" .; then
    error "Failed to build Docker image in Minikube's Docker"
fi

# Verify image exists in Minikube's Docker
log "Images in Minikube's Docker:"
docker images | grep linker-agent || error "Docker image not found in Minikube's Docker after build"

log "✅ Docker image built successfully"

# -------------------------------------------------------------------
# Create namespace and secrets
# -------------------------------------------------------------------

log "🔐 Creating Kubernetes namespace and secrets..."

# Create namespace if it doesn't exist
if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
    kubectl create namespace "$NAMESPACE"
    log "✅ Created namespace: $NAMESPACE"
else
    log "⚡ Namespace $NAMESPACE already exists"
fi

# Delete existing secret if present
if kubectl get secret connektn-secrets -n "$NAMESPACE" >/dev/null 2>&1; then
    log "🔄 Updating existing secret..."
    kubectl delete secret connektn-secrets -n "$NAMESPACE"
fi

# Create secret
kubectl create secret generic connektn-secrets \
    --from-literal=STRIPE_API_KEY="$STRIPE_API_KEY" \
    --from-literal=STRIPE_WEBHOOK_SECRET="$STRIPE_WEBHOOK_SECRET" \
    --from-literal=TENANT_SALT="$TENANT_SALT" \
    --from-literal=ORGANIZATION_ID="$ORGANIZATION_ID" \
    --from-literal=HEARTBEAT_SECRET="$HEARTBEAT_SECRET" \
    --from-literal=CONTROL_SECRET="$CONTROL_SECRET" \
    -n "$NAMESPACE"

log "✅ Secrets created"

# -------------------------------------------------------------------
# Install Connektn Stack
# -------------------------------------------------------------------

log "📦 Installing Connektn stack with Helm..."

# Check if release already exists
if helm list -n "$NAMESPACE" | grep -q "^$RELEASE_NAME"; then
    log "🔄 Upgrading existing Helm release..."
    helm upgrade "$RELEASE_NAME" ./charts/connektn-stack \
        --namespace "$NAMESPACE" \
        --values examples/minikube/values-lite.yaml \
        --set connektn-agent.secrets.existingSecret=connektn-secrets \
        --set connektn-agent.image.pullPolicy=Never
else
    log "🆕 Installing new Helm release..."
    helm install "$RELEASE_NAME" ./charts/connektn-stack \
        --namespace "$NAMESPACE" \
        --values examples/minikube/values-lite.yaml \
        --set connektn-agent.secrets.existingSecret=connektn-secrets \
        --set connektn-agent.image.pullPolicy=Never
fi

log "✅ Helm release deployed"

# -------------------------------------------------------------------
# Wait for pods to be ready
# -------------------------------------------------------------------

log "⏳ Waiting for pods to be ready (this may take a minute)..."

# Wait for agent deployment
kubectl wait --for=condition=available \
    --timeout=120s \
    deployment/connektn-connektn-agent \
    -n "$NAMESPACE" 2>/dev/null || true

# Wait for gateway deployment
kubectl wait --for=condition=available \
    --timeout=120s \
    deployment/connektn-connektn-gateway \
    -n "$NAMESPACE" 2>/dev/null || true

# Show pod status
echo ""
kubectl get pods -n "$NAMESPACE"

# -------------------------------------------------------------------
# Set up port-forward in background
# -------------------------------------------------------------------

log "🔌 Setting up port-forward to gateway..."

# Kill any existing port-forward on 8082
pkill -f "kubectl port-forward.*connektn-connektn-gateway" 2>/dev/null || true

# Start port-forward in background (using 8082 to avoid conflict with CDP platform on 8080)
kubectl port-forward svc/connektn-connektn-gateway 8082:8080 -n "$NAMESPACE" >/dev/null 2>&1 &
PORT_FORWARD_PID=$!

# Wait a moment for port-forward to establish
sleep 2

# Check if port-forward is running
if ! kill -0 $PORT_FORWARD_PID 2>/dev/null; then
    warn "Port-forward failed to start. You can manually start it with:"
    warn "  kubectl port-forward svc/connektn-connektn-gateway 8082:8080 -n $NAMESPACE"
else
    log "✅ Port-forward running in background (PID: $PORT_FORWARD_PID)"
    echo "   Gateway available at: http://localhost:8082"
fi

# -------------------------------------------------------------------
# Success message with next steps
# -------------------------------------------------------------------

echo ""
log "✅ Connektn stack is deployed and running!"
echo ""
echo "📋 Quick test:"
echo ""
echo "   export GATEWAY_URL=http://localhost:8082"
echo "   curl \$GATEWAY_URL/healthz"
echo ""
echo "📚 Useful commands:"
echo ""
echo "   View logs:"
echo "     Agent:   kubectl logs -f deployment/connektn-connektn-agent -n $NAMESPACE"
echo "     Gateway: kubectl logs -f deployment/connektn-connektn-gateway -n $NAMESPACE"
echo ""
echo "   Configure Stripe webhooks:"
echo "     stripe listen --forward-to http://localhost:8082/webhooks/stripe"
echo ""
echo "   Check exported data:"
echo "     kubectl debug -it -n $NAMESPACE <agent-pod> --image=busybox:1.28 --target=agent"
echo ""
echo "   Stop port-forward:"
echo "     kill $PORT_FORWARD_PID"
echo ""
echo "🧹 To cleanup everything: make minikube-down"
echo ""
