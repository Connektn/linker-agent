#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

# -------------------------------------------------------------------
# Connektn Minikube Cleanup Script
# -------------------------------------------------------------------
# Removes all Connektn resources from Minikube and optionally
# stops Minikube itself.
# -------------------------------------------------------------------

NAMESPACE="connektn"
RELEASE_NAME="connektn"
STOP_MINIKUBE="${STOP_MINIKUBE:-false}"

# -------------------------------------------------------------------
# Helpers
# -------------------------------------------------------------------

log() { printf "\n[+] %s\n" "$*"; }
warn() { printf "\n[!] %s\n" "$*" >&2; }

# -------------------------------------------------------------------
# Pre-flight checks
# -------------------------------------------------------------------

log "🔍 Checking environment..."

# Check required tools
for cmd in kubectl helm; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        warn "$cmd not found. Skipping checks that require it."
    fi
done

if ! command -v minikube >/dev/null 2>&1; then
    warn "minikube not found."
    exit 0
fi

# Check if Minikube is running
if ! minikube status >/dev/null 2>&1; then
    log "⚡ Minikube is not running. Nothing to clean up."
    exit 0
fi

log "✅ Minikube is running"

# -------------------------------------------------------------------
# Stop port-forward
# -------------------------------------------------------------------

log "🔌 Stopping port-forward..."
if pkill -f "kubectl port-forward.*connektn-connektn-gateway" 2>/dev/null; then
    log "✅ Port-forward stopped"
else
    log "⚡ No port-forward running"
fi

# -------------------------------------------------------------------
# Uninstall Helm release
# -------------------------------------------------------------------

if command -v helm >/dev/null 2>&1; then
    if helm list -n "$NAMESPACE" 2>/dev/null | grep -q "^$RELEASE_NAME"; then
        log "🗑️  Uninstalling Helm release: $RELEASE_NAME..."
        helm uninstall "$RELEASE_NAME" -n "$NAMESPACE"
        log "✅ Helm release uninstalled"
    else
        log "⚡ Helm release $RELEASE_NAME not found (already removed)"
    fi
else
    warn "Helm not found. Skipping Helm cleanup."
fi

# -------------------------------------------------------------------
# Delete PVC
# -------------------------------------------------------------------

if command -v kubectl >/dev/null 2>&1; then
    if kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
        log "🗑️  Deleting PVCs in namespace $NAMESPACE..."

        # Get all PVCs and delete them
        PVC_COUNT=$(kubectl get pvc -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d ' ')

        if [[ "$PVC_COUNT" -gt 0 ]]; then
            kubectl delete pvc --all -n "$NAMESPACE"
            log "✅ Deleted $PVC_COUNT PVC(s)"
        else
            log "⚡ No PVCs found"
        fi

        # Delete namespace
        log "🗑️  Deleting namespace: $NAMESPACE..."
        kubectl delete namespace "$NAMESPACE"
        log "✅ Namespace deleted"
    else
        log "⚡ Namespace $NAMESPACE not found (already removed)"
    fi
else
    warn "kubectl not found. Skipping Kubernetes resource cleanup."
fi

# -------------------------------------------------------------------
# Reset Docker environment
# -------------------------------------------------------------------

log "🔄 Resetting Docker environment to local daemon..."
eval "$(minikube docker-env -u)"
log "✅ Docker environment reset"

# -------------------------------------------------------------------
# Optionally stop Minikube
# -------------------------------------------------------------------

if [[ "$STOP_MINIKUBE" == "true" ]]; then
    log "🛑 Stopping Minikube..."
    minikube stop
    log "✅ Minikube stopped"
else
    log "⚡ Minikube is still running (use STOP_MINIKUBE=true to stop it)"
fi

# -------------------------------------------------------------------
# Success message
# -------------------------------------------------------------------

echo ""
log "✅ Cleanup complete!"
echo ""
if [[ "$STOP_MINIKUBE" != "true" ]]; then
    echo "💡 Tip: To also stop Minikube, run:"
    echo "   STOP_MINIKUBE=true make minikube-down"
    echo "   or: minikube stop"
    echo ""
fi
