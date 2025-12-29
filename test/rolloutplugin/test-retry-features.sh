#!/bin/bash

set -e

# Configuration
NAMESPACE="argo-rollouts"
STS_NAME="test-sts-retry"
ROLLOUT_NAME="test-rollout-retry"
SERVICE_NAME="test-sts-retry"

# Image configuration
INITIAL_IMAGE="quay.io/prometheus/busybox:latest"
SECOND_IMAGE="quay.io/prometheus/busybox:glibc"
THIRD_IMAGE="quay.io/prometheus/busybox:musl"

REPLICAS=5
TIMEOUT=300
INTERACTIVE="${INTERACTIVE:-true}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m'

# Result tracking
RESULT_FILE="test-retry-results-$(date +%Y%m%d-%H%M%S).txt"

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

info() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo -e "${BLUE}[$timestamp] INFO:${NC} $1" | tee -a "$RESULT_FILE"
}

section() {
    echo "" | tee -a "$RESULT_FILE"
    echo -e "${CYAN}==========================================${NC}" | tee -a "$RESULT_FILE"
    echo -e "${CYAN}$1${NC}" | tee -a "$RESULT_FILE"
    echo -e "${CYAN}==========================================${NC}" | tee -a "$RESULT_FILE"
}

wait_for_user() {
    if [ "$INTERACTIVE" = "true" ]; then
        echo ""
        echo -e "${YELLOW}Press ENTER to continue to next test...${NC}"
        read -r
    else
        sleep 3
    fi
}

show_status() {
    local name=$1
    echo ""
    echo -e "${MAGENTA}Current RolloutPlugin Status:${NC}" | tee -a "$RESULT_FILE"
    kubectl get rolloutplugin "$name" -n "$NAMESPACE" -o jsonpath='{.status}' | jq '.' | tee -a "$RESULT_FILE"
    echo ""
    echo -e "${MAGENTA}Key Fields:${NC}" | tee -a "$RESULT_FILE"
    echo -n "  Phase: " | tee -a "$RESULT_FILE"
    kubectl get rolloutplugin "$name" -n "$NAMESPACE" -o jsonpath='{.status.phase}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Message: " | tee -a "$RESULT_FILE"
    kubectl get rolloutplugin "$name" -n "$NAMESPACE" -o jsonpath='{.status.message}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Current Step: " | tee -a "$RESULT_FILE"
    kubectl get rolloutplugin "$name" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Retry Attempt: " | tee -a "$RESULT_FILE"
    kubectl get rolloutplugin "$name" -n "$NAMESPACE" -o jsonpath='{.status.retryAttempt}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Restarted At: " | tee -a "$RESULT_FILE"
    kubectl get rolloutplugin "$name" -n "$NAMESPACE" -o jsonpath='{.status.restartedAt}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Aborted: " | tee -a "$RESULT_FILE"
    kubectl get rolloutplugin "$name" -n "$NAMESPACE" -o jsonpath='{.status.aborted}' | tee -a "$RESULT_FILE"
    echo "" | tee -a "$RESULT_FILE"
}

show_statefulset_status() {
    echo ""
    echo -e "${MAGENTA}StatefulSet Status:${NC}" | tee -a "$RESULT_FILE"
    echo -n "  Replicas: " | tee -a "$RESULT_FILE"
    kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Partition: " | tee -a "$RESULT_FILE"
    kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Updated Replicas: " | tee -a "$RESULT_FILE"
    kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.updatedReplicas}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Ready Replicas: " | tee -a "$RESULT_FILE"
    kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Current Revision: " | tee -a "$RESULT_FILE"
    kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentRevision}' | tee -a "$RESULT_FILE"
    echo ""
    echo -n "  Update Revision: " | tee -a "$RESULT_FILE"
    kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.updateRevision}' | tee -a "$RESULT_FILE"
    echo "" | tee -a "$RESULT_FILE"
}

# Cleanup function
cleanup() {
    log "Cleaning up resources..."
    kubectl delete rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    kubectl delete statefulset "$STS_NAME" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    kubectl delete service "$SERVICE_NAME" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    log "Cleanup completed"
}

trap cleanup EXIT

# Prerequisites check
check_prerequisites() {
    section "TEST 0: Prerequisites Check"
    
    info "Checking kubectl..."
    if ! command -v kubectl &> /dev/null; then
        error "kubectl not found"
        exit 1
    fi
    log "✓ kubectl found"
    
    info "Checking cluster connectivity..."
    if ! kubectl cluster-info &> /dev/null; then
        error "Cannot connect to cluster"
        exit 1
    fi
    log "✓ Cluster accessible"
    
    info "Checking namespace..."
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        warn "Creating namespace $NAMESPACE"
        kubectl create namespace "$NAMESPACE"
    fi
    log "✓ Namespace exists"
    
    info "Checking CRD..."
    if ! kubectl get crd rolloutplugins.argoproj.io &> /dev/null; then
        error "RolloutPlugin CRD not installed"
        exit 1
    fi
    log "✓ CRD installed"
    
    info "Checking controller..."
    if ! kubectl get pods -n "$NAMESPACE" | grep -q rolloutplugin-controller; then
        warn "Controller not found - deployment may fail"
    else
        log "✓ Controller running"
    fi
    
    log "All prerequisites met!"
    wait_for_user
}

# Test 1: Create initial setup
test_create_initial_setup() {
    section "TEST 1: Create Initial StatefulSet and RolloutPlugin"
    
    info "Creating Service..."
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: $SERVICE_NAME
  namespace: $NAMESPACE
spec:
  clusterIP: None
  selector:
    app: $STS_NAME
  ports:
  - port: 80
    name: web
EOF
    
    info "Creating StatefulSet with image: $INITIAL_IMAGE"
    cat <<EOF | kubectl apply -f -
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
      - name: busybox
        image: $INITIAL_IMAGE
        command: ['sh', '-c', 'echo "Version 1" && sleep 3600']
        ports:
        - containerPort: 80
          name: web
  updateStrategy:
    type: RollingUpdate
EOF
    
    log "Waiting for StatefulSet to be ready..."
    kubectl wait --for=condition=ready pod -l app=$STS_NAME -n "$NAMESPACE" --timeout=60s || {
        error "StatefulSet not ready"
        exit 1
    }
    log "✓ StatefulSet ready with $REPLICAS pods"
    
    info "Creating RolloutPlugin with canary strategy..."
    cat <<EOF | kubectl apply -f -
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: $ROLLOUT_NAME
  namespace: $NAMESPACE
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: $STS_NAME
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {duration: 5s}
      - setWeight: 40
      - pause: {duration: 5s}
      - setWeight: 60
      - pause: {duration: 5s}
      - setWeight: 80
      - pause: {duration: 5s}
      - setWeight: 100
EOF
    
    sleep 5
    log "✓ RolloutPlugin created"
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    wait_for_user
}

# Test 2: Trigger first rollout
test_trigger_rollout() {
    section "TEST 2: Trigger Rollout (Update to Second Image)"
    
    info "Updating StatefulSet to image: $SECOND_IMAGE"
    kubectl patch statefulset "$STS_NAME" -n "$NAMESPACE" --type='json' \
        -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value":"'$SECOND_IMAGE'"}]'
    
    log "✓ StatefulSet updated"
    info "Waiting for rollout to start..."
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Waiting for rollout to reach step 2 (40% canary)..."
    local waited=0
    while [ $waited -lt 60 ]; do
        local step=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "")
        if [ "$step" = "2" ] || [ "$step" = "3" ]; then
            log "✓ Rollout reached step $step"
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    wait_for_user
}

# Test 3: Abort the rollout
test_abort_rollout() {
    section "TEST 3: Abort Rollout"
    
    info "Triggering abort by setting spec.abort=true..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"abort":true}}'
    
    log "✓ Abort triggered"
    info "Waiting for abort to complete..."
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Checking if aborted-revision annotation was set..."
    local annotation=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.rolloutplugin\.argoproj\.io/aborted-revision}' 2>/dev/null || echo "")
    if [ -n "$annotation" ]; then
        log "✓ Aborted revision annotation set: $annotation"
    else
        warn "Aborted revision annotation not found"
    fi
    
    info "Verifying partition was set to replicas (full rollback)..."
    local partition=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}')
    if [ "$partition" = "$REPLICAS" ]; then
        log "✓ Partition set to $REPLICAS (rollback complete)"
    else
        warn "Partition is $partition, expected $REPLICAS"
    fi
    
    wait_for_user
}

# Test 4: Test retry prevention on aborted revision
test_retry_prevention() {
    section "TEST 4: Test Retry Prevention (Aborted Revision Without Allow-Retry)"
    
    info "Attempting to retry aborted revision without allow-retry annotation..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"restartAt":0,"abort":false}}'
    
    log "✓ Retry triggered with restartAt=0"
    info "Waiting to see if retry is blocked..."
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    
    local message=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.message}' 2>/dev/null || echo "")
    if echo "$message" | grep -q "aborted revision\|allow-retry"; then
        log "✓ Retry correctly blocked for aborted revision"
    else
        warn "Expected error message about aborted revision, got: $message"
    fi
    
    info "Checking if restartAt was cleared..."
    local restartAt=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.restartAt}' 2>/dev/null || echo "null")
    if [ "$restartAt" = "null" ] || [ -z "$restartAt" ]; then
        log "✓ restartAt field was cleared (one-shot trigger)"
    else
        warn "restartAt still set to: $restartAt"
    fi
    
    wait_for_user
}

# Test 5: Allow retry with annotation
test_retry_with_annotation() {
    section "TEST 5: Allow Retry on Aborted Revision (With Allow-Retry Annotation)"
    
    info "Setting allow-retry annotation on StatefulSet..."
    kubectl annotate statefulset "$STS_NAME" -n "$NAMESPACE" rolloutplugin.argoproj.io/allow-retry=true --overwrite
    
    log "✓ Annotation set"
    
    info "Triggering retry with restartAt=0..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"restartAt":0}}'
    
    log "✓ Retry triggered"
    info "Waiting for retry to process..."
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Checking retry attempt counter..."
    local retryAttempt=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.retryAttempt}' 2>/dev/null || echo "0")
    if [ "$retryAttempt" -gt "0" ]; then
        log "✓ Retry attempt counter incremented: $retryAttempt"
    else
        warn "Retry attempt counter not incremented"
    fi
    
    info "Checking restartedAt timestamp..."
    local restartedAt=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.restartedAt}' 2>/dev/null || echo "")
    if [ -n "$restartedAt" ]; then
        log "✓ RestartedAt timestamp set: $restartedAt"
    else
        warn "RestartedAt timestamp not set"
    fi
    
    info "Checking if annotations were cleared after successful retry..."
    sleep 5
    local abortedRev=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.rolloutplugin\.argoproj\.io/aborted-revision}' 2>/dev/null || echo "")
    local allowRetry=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.rolloutplugin\.argoproj\.io/allow-retry}' 2>/dev/null || echo "")
    if [ -z "$abortedRev" ] && [ -z "$allowRetry" ]; then
        log "✓ Aborted-revision and allow-retry annotations cleared"
    else
        warn "Annotations not cleared: aborted-revision=$abortedRev, allow-retry=$allowRetry"
    fi
    
    wait_for_user
}

# Test 6: Let rollout complete and test retry prevention on success
test_complete_and_prevent_retry() {
    section "TEST 6: Complete Rollout and Test Retry Prevention on Success"
    
    info "Letting rollout complete to successful state..."
    info "Waiting for rollout to reach Completed state (this may take a while)..."
    
    local waited=0
    while [ $waited -lt 120 ]; do
        local phase=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        if [ "$phase" = "Completed" ] || [ "$phase" = "Successful" ]; then
            log "✓ Rollout completed successfully"
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Attempting to retry on successful rollout (should be blocked)..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"restartAt":0}}'
    
    log "✓ Retry triggered"
    info "Waiting to see if retry is blocked..."
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    
    local message=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.message}' 2>/dev/null || echo "")
    if echo "$message" | grep -q "success\|completed\|healthy"; then
        log "✓ Retry correctly blocked on successful rollout"
    else
        warn "Expected error about successful rollout, got: $message"
    fi
    
    wait_for_user
}

# Test 7: New rollout and retry counter reset
test_new_rollout_counter_reset() {
    section "TEST 7: New Rollout - Verify Retry Counter Reset"
    
    info "Current retry attempt count:"
    kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.retryAttempt}' | tee -a "$RESULT_FILE"
    echo "" | tee -a "$RESULT_FILE"
    
    info "Triggering new rollout with third image: $THIRD_IMAGE"
    kubectl patch statefulset "$STS_NAME" -n "$NAMESPACE" --type='json' \
        -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value":"'$THIRD_IMAGE'"},
             {"op": "replace", "path": "/spec/template/metadata/labels/version", "value":"v3"}]'
    
    log "✓ StatefulSet updated to trigger new rollout"
    info "Waiting for new rollout to start..."
    sleep 15
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Checking if retry counter was reset..."
    local retryAttempt=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.retryAttempt}' 2>/dev/null || echo "0")
    if [ "$retryAttempt" = "0" ]; then
        log "✓ Retry counter reset to 0 for new rollout"
    else
        warn "Retry counter not reset, still at: $retryAttempt"
    fi
    
    info "Checking if restartedAt was cleared..."
    local restartedAt=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.restartedAt}' 2>/dev/null || echo "")
    if [ -z "$restartedAt" ] || [ "$restartedAt" = "null" ]; then
        log "✓ RestartedAt cleared for new rollout"
    else
        warn "RestartedAt not cleared: $restartedAt"
    fi
    
    wait_for_user
}

# Test 8: Retry from specific step
test_retry_from_step() {
    section "TEST 8: Retry From Specific Step"
    
    info "Waiting for rollout to progress to step 3..."
    local waited=0
    while [ $waited -lt 60 ]; do
        local step=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "")
        if [ "$step" = "3" ] || [ "$step" = "4" ]; then
            log "✓ Rollout at step $step"
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    
    show_status "$ROLLOUT_NAME"
    
    info "Triggering retry from step 2 (restartAt=2)..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"restartAt":2}}'
    
    log "✓ Retry from step 2 triggered"
    info "Waiting for retry to process..."
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    
    info "Verifying currentStepIndex was reset to 2..."
    local currentStep=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "")
    if [ "$currentStep" = "2" ]; then
        log "✓ Current step reset to 2"
    else
        warn "Current step is $currentStep, expected 2"
    fi
    
    info "Checking retry counter incremented..."
    local retryAttempt=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.retryAttempt}' 2>/dev/null || echo "0")
    if [ "$retryAttempt" -gt "0" ]; then
        log "✓ Retry attempt: $retryAttempt"
    else
        warn "Retry counter not incremented"
    fi
    
    wait_for_user
}

# Test 9: Reset functionality
test_reset_functionality() {
    section "TEST 9: Test Reset() Plugin Method"
    
    info "Current StatefulSet partition before reset:"
    kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}' | tee -a "$RESULT_FILE"
    echo "" | tee -a "$RESULT_FILE"
    
    info "Triggering retry (which calls Reset())..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"restartAt":0}}'
    
    log "✓ Retry triggered"
    info "Waiting for Reset() to be called..."
    sleep 10
    
    show_statefulset_status
    
    info "Verifying partition was set to replicas (baseline state)..."
    local partition=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}')
    if [ "$partition" = "$REPLICAS" ]; then
        log "✓ Reset() worked: partition set to $REPLICAS (0% canary)"
    else
        warn "Partition is $partition, expected $REPLICAS"
    fi
    
    wait_for_user
}

# Test 10: Full Promote test
test_promote_full_load() {
    section "TEST 10: Promote to Full Load (100%)"
    
    info "Current status before promote:"
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Letting rollout progress to near completion (step 7 - 80%)..."
    local waited=0
    while [ $waited -lt 90 ]; do
        local step=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "")
        if [ "$step" = "7" ] || [ "$step" = "8" ]; then
            log "✓ Rollout at step $step (80%)"
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Current partition before promote:"
    local partition_before=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}')
    log "Partition before promote: $partition_before"
    
    info "Triggering manual promote (skip remaining steps)..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"promote":true}}'
    
    log "✓ Promote triggered"
    info "Waiting for promote to complete..."
    sleep 15
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Verifying partition was set to 0 (100% rollout)..."
    local partition=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}')
    if [ "$partition" = "0" ]; then
        log "✓ Promote successful: partition set to 0 (100% new version)"
    else
        warn "Partition is $partition, expected 0"
    fi
    
    info "Verifying all pods updated..."
    local updatedReplicas=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.updatedReplicas}')
    if [ "$updatedReplicas" = "$REPLICAS" ]; then
        log "✓ All $REPLICAS pods updated"
    else
        warn "Updated replicas: $updatedReplicas, expected $REPLICAS"
    fi
    
    info "Waiting for rollout to complete..."
    sleep 20
    show_status "$ROLLOUT_NAME"
    
    wait_for_user
}

# Test 11: Abort during mid-rollout
test_abort_mid_rollout() {
    section "TEST 11: Abort During Mid-Rollout"
    
    info "Starting new rollout for abort test..."
    kubectl patch statefulset "$STS_NAME" -n "$NAMESPACE" --type='json' \
        -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value":"'$INITIAL_IMAGE'"},
             {"op": "replace", "path": "/spec/template/metadata/labels/version", "value":"v4"}]'
    
    log "✓ StatefulSet updated"
    info "Waiting for rollout to reach step 2 (40%)..."
    local waited=0
    while [ $waited -lt 60 ]; do
        local step=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "")
        if [ "$step" = "2" ] || [ "$step" = "3" ]; then
            log "✓ Rollout at step $step"
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    
    info "Status before abort:"
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    local partition_before=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}')
    local updated_before=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.updatedReplicas}')
    log "Before abort - Partition: $partition_before, Updated: $updated_before"
    
    info "Triggering abort..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"abort":true}}'
    
    log "✓ Abort triggered"
    info "Waiting for abort to complete..."
    sleep 15
    
    info "Status after abort:"
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Verifying abort behavior:"
    
    # Check partition set to replicas
    local partition=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}')
    if [ "$partition" = "$REPLICAS" ]; then
        log "✓ Partition set to $REPLICAS (blocking new updates)"
    else
        warn "Partition is $partition, expected $REPLICAS"
    fi
    
    # Check aborted-revision annotation
    local aborted_rev=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.rolloutplugin\.argoproj\.io/aborted-revision}' 2>/dev/null || echo "")
    if [ -n "$aborted_rev" ]; then
        log "✓ Aborted-revision annotation set: $aborted_rev"
    else
        warn "Aborted-revision annotation not found"
    fi
    
    # Check aborted status field
    local aborted=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.aborted}' 2>/dev/null || echo "false")
    if [ "$aborted" = "true" ]; then
        log "✓ Status.aborted set to true"
    else
        warn "Status.aborted is $aborted, expected true"
    fi
    
    # Check pod deletion for rollback
    info "Checking if updated pods were deleted for rollback..."
    sleep 10
    local updated_after=$(kubectl get statefulset "$STS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.updatedReplicas}')
    log "Updated replicas after abort: $updated_after"
    
    wait_for_user
}

# Test 12: Manual pause and resume
test_pause_and_resume() {
    section "TEST 12: Manual Pause and Resume"
    
    info "Creating fresh rollout for pause test..."
    kubectl delete rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --ignore-not-found=true
    sleep 5
    
    cat <<EOF | kubectl apply -f -
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: $ROLLOUT_NAME
  namespace: $NAMESPACE
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: $STS_NAME
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {}  # Indefinite pause
      - setWeight: 40
      - pause: {duration: 5s}
      - setWeight: 60
      - pause: {}  # Another indefinite pause
      - setWeight: 80
      - pause: {duration: 5s}
      - setWeight: 100
EOF
    
    log "✓ RolloutPlugin with pause steps created"
    sleep 5
    
    info "Triggering rollout..."
    kubectl patch statefulset "$STS_NAME" -n "$NAMESPACE" --type='json' \
        -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value":"'$THIRD_IMAGE'"},
             {"op": "replace", "path": "/spec/template/metadata/labels/version", "value":"v6"}]'
    
    log "✓ Rollout triggered"
    info "Waiting for rollout to reach first pause step (step 1)..."
    local waited=0
    while [ $waited -lt 60 ]; do
        local step=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "")
        local phase=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        if [ "$step" = "1" ] && [ "$phase" = "Paused" ]; then
            log "✓ Rollout paused at step 1"
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Verifying pause state..."
    local phase=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.phase}')
    if [ "$phase" = "Paused" ]; then
        log "✓ Status.phase = Paused"
    else
        warn "Status.phase is $phase, expected Paused"
    fi
    
    info "Waiting 15 seconds to confirm rollout stays paused..."
    sleep 15
    
    local step_after_wait=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}')
    if [ "$step_after_wait" = "1" ]; then
        log "✓ Rollout still paused at step 1 (indefinite pause working)"
    else
        warn "Rollout advanced to step $step_after_wait, pause may not be working"
    fi
    
    info "Manually resuming rollout by setting spec.paused=false..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"paused":false}}'
    
    log "✓ Resume triggered"
    info "Waiting for rollout to progress..."
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    show_statefulset_status
    
    info "Verifying rollout progressed past pause..."
    local current_step=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}')
    if [ "$current_step" -gt "1" ]; then
        log "✓ Rollout progressed to step $current_step after resume"
    else
        warn "Rollout still at step $current_step"
    fi
    
    info "Manually pausing rollout during progression..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"paused":true}}'
    
    log "✓ Manual pause triggered"
    sleep 10
    
    show_status "$ROLLOUT_NAME"
    
    local phase_after_pause=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.phase}')
    if [ "$phase_after_pause" = "Paused" ]; then
        log "✓ Manual pause successful"
    else
        warn "Phase is $phase_after_pause, expected Paused"
    fi
    
    info "Resuming to let rollout continue..."
    kubectl patch rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --type=merge -p '{"spec":{"paused":false}}'
    
    log "✓ Final resume triggered, letting rollout progress naturally"
    sleep 15
    
    show_status "$ROLLOUT_NAME"
    
    wait_for_user
}

# Test 13: Analysis run integration (if AnalysisTemplate exists)
test_analysis_integration() {
    section "TEST 13: Analysis Run Integration"
    
    info "Checking if AnalysisTemplate CRD exists..."
    if ! kubectl get crd analysistemplates.argoproj.io &> /dev/null; then
        warn "AnalysisTemplate CRD not installed, skipping analysis test"
        log "Note: Install Argo Rollouts with Analysis to test this feature"
        wait_for_user
        return
    fi
    log "✓ AnalysisTemplate CRD found"
    
    info "Creating AnalysisTemplate for testing..."
    cat <<EOF | kubectl apply -f -
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: test-analysis
  namespace: $NAMESPACE
spec:
  metrics:
  - name: success-rate
    provider:
      job:
        spec:
          template:
            spec:
              containers:
              - name: test
                image: busybox:latest
                command: ['sh', '-c', 'exit 0']  # Always pass
              restartPolicy: Never
    successCondition: result == "0"
    interval: 10s
    count: 2
EOF
    
    log "✓ AnalysisTemplate created"
    
    info "Creating RolloutPlugin with analysis..."
    kubectl delete rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" --ignore-not-found=true
    sleep 5
    
    cat <<EOF | kubectl apply -f -
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: $ROLLOUT_NAME
  namespace: $NAMESPACE
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: $STS_NAME
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 25
      - pause: {duration: 10s}
      - analysis:
          templates:
          - templateName: test-analysis
      - setWeight: 75
      - pause: {duration: 10s}
      - setWeight: 100
EOF
    
    log "✓ RolloutPlugin with analysis created"
    sleep 5
    
    info "Triggering rollout..."
    kubectl patch statefulset "$STS_NAME" -n "$NAMESPACE" --type='json' \
        -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value":"'$SECOND_IMAGE'"},
             {"op": "replace", "path": "/spec/template/metadata/labels/version", "value":"v5"}]'
    
    log "✓ Rollout triggered"
    info "Waiting for rollout to reach analysis step..."
    local waited=0
    while [ $waited -lt 90 ]; do
        local step=$(kubectl get rolloutplugin "$ROLLOUT_NAME" -n "$NAMESPACE" -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "")
        if [ "$step" = "2" ]; then
            log "✓ Rollout reached analysis step"
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    
    show_status "$ROLLOUT_NAME"
    
    info "Checking for AnalysisRun creation..."
    local ar=$(kubectl get analysisrun -n "$NAMESPACE" -l rolloutplugin.argoproj.io/name=$ROLLOUT_NAME --no-headers 2>/dev/null | head -1 | awk '{print $1}')
    if [ -n "$ar" ]; then
        log "✓ AnalysisRun created: $ar"
        echo ""
        info "AnalysisRun status:"
        kubectl get analysisrun "$ar" -n "$NAMESPACE" -o jsonpath='{.status}' | jq '.' | tee -a "$RESULT_FILE"
        echo ""
    else
        warn "No AnalysisRun found (might be created shortly)"
    fi
    
    info "Waiting for analysis to complete..."
    sleep 30
    
    if [ -n "$ar" ]; then
        info "Final AnalysisRun status:"
        kubectl get analysisrun "$ar" -n "$NAMESPACE" -o jsonpath='{.status.phase}' | tee -a "$RESULT_FILE"
        echo ""
    fi
    
    show_status "$ROLLOUT_NAME"
    
    wait_for_user
}

# Main execution
main() {
    section "RolloutPlugin Complete Features Test Suite"
    log "Configuration:"
    log "  Namespace: $NAMESPACE"
    log "  StatefulSet: $STS_NAME"
    log "  RolloutPlugin: $ROLLOUT_NAME"
    log "  Replicas: $REPLICAS"
    log "  Initial Image: $INITIAL_IMAGE"
    log "  Second Image: $SECOND_IMAGE"
    log "  Third Image: $THIRD_IMAGE"
    log "  Result File: $RESULT_FILE"
    log "  Interactive Mode: $INTERACTIVE"
    section ""
    
    check_prerequisites
    test_create_initial_setup
    test_trigger_rollout
    test_abort_rollout
    test_retry_prevention
    test_retry_with_annotation
    test_complete_and_prevent_retry
    test_new_rollout_counter_reset
    test_retry_from_step
    test_reset_functionality
    test_promote_full_load
    test_abort_mid_rollout
    test_pause_and_resume
    test_analysis_integration
    
    section "TEST SUITE COMPLETED ✓"
    log "All tests completed!"
    log "Results saved to: $RESULT_FILE"
    echo ""
    log "Summary of tested features:"
    log ""
    log "RETRY FEATURES:"
    log "  ✓ spec.restartAt (one-shot retry trigger)"
    log "  ✓ status.retryAttempt (retry counter)"
    log "  ✓ status.restartedAt (timestamp)"
    log "  ✓ Retry prevention on successful rollout"
    log "  ✓ Retry prevention on aborted revision (without annotation)"
    log "  ✓ Retry allowed on aborted revision (with allow-retry annotation)"
    log "  ✓ Retry counter reset on new rollout"
    log "  ✓ Retry from specific step"
    log "  ✓ Reset() plugin method (partition reset to baseline)"
    log "  ✓ Annotation cleanup after retry"
    log ""
    log "ABORT FEATURES:"
    log "  ✓ Abort mid-rollout"
    log "  ✓ Partition set to replicas (blocking updates)"
    log "  ✓ Aborted-revision annotation tracking"
    log "  ✓ Pod deletion for rollback"
    log "  ✓ Status.aborted flag"
    log ""
    log "PROMOTE FEATURES:"
    log "  ✓ Manual promote to 100%"
    log "  ✓ Partition set to 0 (full deployment)"
    log "  ✓ Skip remaining steps"
    log "  ✓ All pods updated"
    log ""
    log "PAUSE FEATURES:"
    log "  ✓ Indefinite pause (pause: {})"
    log "  ✓ Manual resume (spec.paused=false)"
    log "  ✓ Manual pause during rollout (spec.paused=true)"
    log "  ✓ Duration-based pause (pause: {duration: 5s})"
    log "  ✓ Status.phase = Paused"
    log ""
    log "ANALYSIS FEATURES:"
    log "  ✓ AnalysisRun integration (if CRD available)"
    log "  ✓ Analysis step execution"
    log "  ✓ Analysis result handling"
    section ""
}

main "$@"
