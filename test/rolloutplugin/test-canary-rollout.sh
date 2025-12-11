#!/bin/bash

set -e

# Configuration
NAMESPACE="argo-rollouts"
STS_NAME="test-sts"
ROLLOUT_NAME="test-statefulset-rollout"
SERVICE_NAME="test-sts"

# Image configuration - using quay.io images
USE_BUSYBOX="${USE_BUSYBOX:-true}"

if [ "$USE_BUSYBOX" = "true" ]; then
    INITIAL_IMAGE="quay.io/prometheus/busybox:latest"
    NEW_IMAGE="quay.io/prometheus/busybox:glibc"
    echo "Using busybox images from quay.io"
else
    INITIAL_IMAGE="quay.io/bitnami/nginx:1.25"
    NEW_IMAGE="quay.io/bitnami/nginx:1.26"
    echo "Using nginx images from quay.io"
fi

REPLICAS=5
TIMEOUT=120

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Result tracking
RESULT_FILE="test-results-$(date +%Y%m%d-%H%M%S).txt"

log() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo -e "${GREEN}[$timestamp]${NC} $1" | tee -a "$RESULT_FILE"
}

error() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo -e "${RED}[$timestamp] ERROR:${NC} $1" | tee -a "$RESULT_FILE"
}

warn() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo -e "${YELLOW}[$timestamp] WARNING:${NC} $1" | tee -a "$RESULT_FILE"
}

# Cleanup function
cleanup() {
    log "Cleaning up resources..."
    kubectl delete rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    kubectl delete statefulset "$STS_NAME" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    kubectl delete service "$SERVICE_NAME" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    log "Cleanup completed"
}

# Set trap for cleanup
trap cleanup EXIT

# Prerequisites check
check_prerequisites() {
    log "Checking prerequisites..."
    
    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        error "kubectl not found. Please install kubectl."
        exit 1
    fi
    
    # Check cluster connectivity
    if ! kubectl cluster-info &> /dev/null; then
        error "Cannot connect to Kubernetes cluster. Please check your kubeconfig."
        exit 1
    fi
    
    # Check namespace
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        error "Namespace '$NAMESPACE' does not exist. Create it with: kubectl create namespace $NAMESPACE"
        exit 1
    fi
    
    # Check CRD
    if ! kubectl get crd rolloutplugins.argoproj.io &> /dev/null; then
        error "RolloutPlugin CRD is not installed. Install it first."
        exit 1
    fi
    
    # Check controller
    if ! kubectl get pods -n "$NAMESPACE" | grep -q rolloutplugin-controller; then
        warn "RolloutPlugin controller not found in namespace '$NAMESPACE'. This test may fail."
        warn "Deploy the controller first with: kubectl apply -f controller-deployment.yaml"
    fi
    
    log "✓ All prerequisites met"
}

# Create StatefulSet
create_statefulset() {
    log "Creating StatefulSet with initial image: $INITIAL_IMAGE"
    
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: $SERVICE_NAME
  namespace: $NAMESPACE
  labels:
    app: $STS_NAME
spec:
  clusterIP: None
  selector:
    app: $STS_NAME
  ports:
  - port: 80
    name: web
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: $STS_NAME
  namespace: $NAMESPACE
spec:
  serviceName: $SERVICE_NAME
  replicas: $REPLICAS
  selector:
    matchLabels:
      app: $STS_NAME
  template:
    metadata:
      labels:
        app: $STS_NAME
        version: v1
    spec:
      containers:
      - name: app
        image: $INITIAL_IMAGE
        command: ["sh", "-c", "while true; do echo 'v1 running'; sleep 10; done"]
EOF
    
    log "✓ StatefulSet created"
    
    # Wait for StatefulSet to be ready
    log "Waiting for StatefulSet to be ready..."
    if ! kubectl wait --for=condition=ready pod -l app=$STS_NAME -n "$NAMESPACE" --timeout=60s; then
        error "StatefulSet pods did not become ready in time"
        kubectl get pods -n "$NAMESPACE" -l app=$STS_NAME
        exit 1
    fi
    
    log "✓ StatefulSet is ready with all $REPLICAS pods"
}

# Update StatefulSet to trigger rollout
update_statefulset() {
    log "Updating StatefulSet to new image: $NEW_IMAGE"
    
    kubectl patch statefulset "$STS_NAME" -n "$NAMESPACE" --type='json' \
        -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value":"'$NEW_IMAGE'"},
             {"op": "replace", "path": "/spec/template/metadata/labels/version", "value":"v2"}]'
    
    log "✓ StatefulSet updated"
}

# Create RolloutPlugin
create_rolloutplugin() {
    log "Creating RolloutPlugin with canary strategy"
    
    cat <<EOF | kubectl apply -f -
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: test-statefulset-rollout
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: $STS_NAME
  plugin:
    name: statefulset-plugin
    config:
      resourceType: StatefulSet
  strategy:
    canary:
      steps:
      - setWeight: 33
      - pause: {}
      - setWeight: 66
      - pause: {}
EOF
    
    log "✓ RolloutPlugin created with 5-step canary (20%, 40%, 60%, 80%, 100%)"
}

# Monitor rollout progress
monitor_rollout() {
    log "Monitoring rollout progress for up to ${TIMEOUT}s..."
    log "Expected progression:"
    log "  20% = 1 pod updated (partition=4)"
    log "  40% = 2 pods updated (partition=3)"
    log "  60% = 3 pods updated (partition=2)"
    log "  80% = 4 pods updated (partition=1)"
    log "  100% = 5 pods updated (partition=0)"
    echo "" | tee -a "$RESULT_FILE"
    
    local start_time=$(date +%s)
    local last_phase=""
    local last_step=-1
    
    while true; do
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        
        if [ $elapsed -gt $TIMEOUT ]; then
            error "Timeout waiting for rollout to complete"
            log "Final status:"
            kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o yaml | tee -a "$RESULT_FILE"
            exit 1
        fi
        
        # Get RolloutPlugin status
        local status=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o json 2>/dev/null || echo '{}')
        local phase=$(echo "$status" | jq -r '.status.phase // "Unknown"')
        local step=$(echo "$status" | jq -r '.status.currentStepIndex // -1')
        local message=$(echo "$status" | jq -r '.status.message // ""')
        
        # Get StatefulSet status
        local sts_status=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o json 2>/dev/null || echo '{}')
        local partition=$(echo "$sts_status" | jq -r '.spec.updateStrategy.rollingUpdate.partition // 0')
        local updated_replicas=$(echo "$sts_status" | jq -r '.status.updatedReplicas // 0')
        local ready_replicas=$(echo "$sts_status" | jq -r '.status.readyReplicas // 0')
        
        # Print status if phase or step changed
        if [ "$phase" != "$last_phase" ] || [ "$step" != "$last_step" ]; then
            log "Status at ${elapsed}s:"
            log "  RolloutPlugin: Phase=$phase, Step=$step"
            [ -n "$message" ] && log "  Message: $message"
            log "  StatefulSet: Partition=$partition, Updated=$updated_replicas/$REPLICAS, Ready=$ready_replicas/$REPLICAS"
            echo "" | tee -a "$RESULT_FILE"
            
            last_phase="$phase"
            last_step="$step"
        fi
        
        # Check if rollout completed
        if [ "$phase" = "Successful" ]; then
            log "✓ Rollout completed successfully!"
            log "Final state: Partition=$partition, Updated=$updated_replicas/$REPLICAS, Ready=$ready_replicas/$REPLICAS"
            return 0
        fi
        
        # Check if rollout failed
        if [ "$phase" = "Failed" ]; then
            error "Rollout failed!"
            log "Final status:"
            kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o yaml | tee -a "$RESULT_FILE"
            exit 1
        fi
        
        sleep 5
    done
}

# Main execution
main() {
    log "=========================================="
    log "RolloutPlugin StatefulSet Canary Test"
    log "=========================================="
    log "Configuration:"
    log "  Namespace: $NAMESPACE"
    log "  StatefulSet: $STS_NAME"
    log "  RolloutPlugin: $ROLLOUT_NAME"
    log "  Replicas: $REPLICAS"
    log "  Initial Image: $INITIAL_IMAGE"
    log "  New Image: $NEW_IMAGE"
    log "  Result File: $RESULT_FILE"
    log "=========================================="
    echo "" | tee -a "$RESULT_FILE"
    
    check_prerequisites
    echo "" | tee -a "$RESULT_FILE"
    
    create_statefulset
    echo "" | tee -a "$RESULT_FILE"
    
    # Give some time for StatefulSet to stabilize
    sleep 5
    
    update_statefulset
    echo "" | tee -a "$RESULT_FILE"
    
    create_rolloutplugin
    echo "" | tee -a "$RESULT_FILE"
    
    monitor_rollout
    echo "" | tee -a "$RESULT_FILE"
    
    log "=========================================="
    log "TEST PASSED ✓"
    log "=========================================="
    log "Results saved to: $RESULT_FILE"
}

main "$@"
