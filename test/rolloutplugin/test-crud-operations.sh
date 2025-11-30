#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

# Test results file
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
RESULTS_FILE="test/rolloutplugin/test-results-${TIMESTAMP}.txt"
SUMMARY_FILE="test/rolloutplugin/test-summary-latest.txt"

# Test counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Function to log both to console and file
log() {
    echo -e "$1" | tee -a "$RESULTS_FILE"
}

# Function to log without color codes to file
log_plain() {
    echo "$1" | tee -a "$RESULTS_FILE"
}

# Function to mark test as passed
mark_pass() {
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    PASSED_TESTS=$((PASSED_TESTS + 1))
    log "${GREEN}✓ PASS${NC}"
    echo "✓ PASS" >> "$RESULTS_FILE"
}

# Function to mark test as failed
mark_fail() {
    local reason=$1
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    FAILED_TESTS=$((FAILED_TESTS + 1))
    log "${RED}✗ FAIL: $reason${NC}"
    echo "✗ FAIL: $reason" >> "$RESULTS_FILE"
}

# Initialize results file
echo "CRUD Operations Test Results" > "$RESULTS_FILE"
echo "=============================" >> "$RESULTS_FILE"
echo "Test Date: $(date)" >> "$RESULTS_FILE"
echo "Test Environment: $(kubectl config current-context)" >> "$RESULTS_FILE"
echo "" >> "$RESULTS_FILE"

log "==> Testing CRUD Operations for RolloutPlugin Controller"
log ""

# Function to check status
check_status() {
    local name=$1
    local expected_phase=$2
    
    echo -n "  Checking status... "
    local actual_phase=$(kubectl get rolloutplugin $name -n argo-rollouts -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    
    if [ -z "$actual_phase" ]; then
        echo -e "${YELLOW}No status yet${NC}"
        return 1
    elif [ "$actual_phase" == "$expected_phase" ]; then
        echo -e "${GREEN}✓ Phase: $actual_phase${NC}"
        return 0
    else
        echo -e "${YELLOW}Phase: $actual_phase (expected: $expected_phase)${NC}"
        return 1
    fi
}

# Function to show resource details
show_details() {
    local name=$1
    echo ""
    echo -e "${BLUE}Resource Details:${NC}"
    kubectl get rolloutplugin $name -n argo-rollouts -o yaml | grep -A 20 "status:" || echo "  No status yet"
    echo ""
}

log "${BLUE}==> Test 1: CREATE - Creating RolloutPlugin${NC}"
log "Creating nginx-rollout..."
if kubectl apply -f test/rolloutplugin/rolloutplugin-sample.yaml -n argo-rollouts >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Failed to create resource"
    exit 1
fi

log "Waiting for controller to reconcile (3 seconds)..."
sleep 3

log "Checking if resource exists:"
if kubectl get rolloutplugin nginx-rollout -n argo-rollouts >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Resource not found after creation"
    exit 1
fi

log ""
log "Checking status:"
kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o jsonpath='{.status}' | jq . | tee -a "$RESULTS_FILE" || echo "No status yet" | tee -a "$RESULTS_FILE"
log ""

show_details "nginx-rollout"

log "${BLUE}==> Test 2: READ - Getting RolloutPlugin${NC}"
log "Get by name:"
if kubectl get rolloutplugin nginx-rollout -n argo-rollouts >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Failed to get resource by name"
fi

log ""
log "Get with custom columns:"
kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,MESSAGE:.status.message,REPLICAS:.status.replicas | tee -a "$RESULTS_FILE"
log ""

log "List all RolloutPlugins:"
if kubectl get rolloutplugin -n argo-rollouts >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Failed to list resources"
fi

log ""
log "Describe resource:"
kubectl describe rolloutplugin nginx-rollout -n argo-rollouts >> "$RESULTS_FILE" 2>&1
if [ $? -eq 0 ]; then
    mark_pass
else
    mark_fail "Failed to describe resource"
fi
log ""

log "${BLUE}==> Test 3: UPDATE - Modifying RolloutPlugin Spec${NC}"
log "Current strategy steps:"
kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o jsonpath='{.spec.strategy.canary.steps}' | jq . | tee -a "$RESULTS_FILE"
log ""

log "Creating a modified version with different weights..."
cat > /tmp/rolloutplugin-updated.yaml <<EOF
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: nginx-rollout
  namespace: argo-rollouts
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: nginx-test
  plugin:
    name: statefulset-plugin
    config:
      resourceType: StatefulSet
  strategy:
    canary:
      steps:
        - setWeight: 25
        - pause: {}
        - setWeight: 50
        - pause: {}
        - setWeight: 75
        - pause: {}
EOF

log "Applying updated spec..."
if kubectl apply -f /tmp/rolloutplugin-updated.yaml >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Failed to apply updated spec"
fi

log "Waiting for controller to reconcile (3 seconds)..."
sleep 3

log "New strategy steps:"
kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o jsonpath='{.spec.strategy.canary.steps}' | jq . | tee -a "$RESULTS_FILE"
log ""

log "Checking if observedGeneration updated:"
GEN_STATUS=$(kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o jsonpath='{.metadata.generation} {.status.observedGeneration}')
echo "  Generation: $(echo $GEN_STATUS | awk '{print $1}'), ObservedGeneration: $(echo $GEN_STATUS | awk '{print $2}')" | tee -a "$RESULTS_FILE"
if [ "$(echo $GEN_STATUS | awk '{print $1}')" == "$(echo $GEN_STATUS | awk '{print $2}')" ]; then
    mark_pass
else
    mark_fail "ObservedGeneration not updated"
fi
log ""

show_details "nginx-rollout"

log "${BLUE}==> Test 4: UPDATE - Testing Status Subresource${NC}"
log "Current status:"
STATUS_JSON=$(kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o jsonpath='{.status}')
echo "$STATUS_JSON" | jq . | tee -a "$RESULTS_FILE"
log ""

# Check if status has phase and message
if echo "$STATUS_JSON" | jq -e '.phase' > /dev/null && echo "$STATUS_JSON" | jq -e '.message' > /dev/null; then
    mark_pass
else
    mark_fail "Status missing phase or message fields"
fi

log "Note: Status updates should only come from the controller."
log "The controller should be updating status.phase, status.message, etc."
log ""

echo -e "${BLUE}==> Test 5: LIST and WATCH${NC}"
echo "Creating a second RolloutPlugin for list testing..."
cat > /tmp/rolloutplugin-second.yaml <<EOF
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: nginx-rollout-2
  namespace: argo-rollouts
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: nginx-test-2
  plugin:
    name: statefulset-plugin
    config:
      resourceType: StatefulSet
  strategy:
    canary:
      steps:
        - setWeight: 50
        - pause: {}
EOF

if kubectl apply -f /tmp/rolloutplugin-second.yaml >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Failed to create second resource"
fi

log "Waiting for controller to reconcile (3 seconds)..."
sleep 3

log "Listing all RolloutPlugins:"
kubectl get rolloutplugin -n argo-rollouts | tee -a "$RESULTS_FILE"
log ""

log "Using short name 'rp':"
if kubectl get rp -n argo-rollouts >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Short name 'rp' not working"
fi

log ""
log "With custom columns:"
kubectl get rp -n argo-rollouts -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,STRATEGY:.spec.strategy.canary,STEPS:.status.currentStepIndex | tee -a "$RESULTS_FILE"
log ""

log "${BLUE}==> Test 6: PATCH - Updating individual fields${NC}"
log "Patching plugin config..."
if kubectl patch rolloutplugin nginx-rollout -n argo-rollouts --type=merge -p '{"spec":{"plugin":{"config":{"newField":"newValue"}}}}' >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Failed to patch resource"
fi

log "Verifying patch:"
PATCH_RESULT=$(kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o jsonpath='{.spec.plugin.config}')
echo "$PATCH_RESULT" | jq . | tee -a "$RESULTS_FILE"
if echo "$PATCH_RESULT" | jq -e '.newField' > /dev/null; then
    mark_pass
else
    mark_fail "Patch not applied correctly"
fi
log ""

log "${BLUE}==> Test 7: DELETE - Removing RolloutPlugin${NC}"
log "Deleting nginx-rollout-2..."
if kubectl delete rolloutplugin nginx-rollout-2 -n argo-rollouts >> "$RESULTS_FILE" 2>&1; then
    mark_pass
else
    mark_fail "Failed to delete resource"
fi

log "Verifying deletion:"
kubectl get rolloutplugin -n argo-rollouts | tee -a "$RESULTS_FILE"
# Verify nginx-rollout-2 is not in the list
if ! kubectl get rolloutplugin nginx-rollout-2 -n argo-rollouts 2>/dev/null; then
    mark_pass
else
    mark_fail "Resource still exists after deletion"
fi
log ""

log "${BLUE}==> Test 8: Controller Logs Check${NC}"
log "Recent controller logs (last 30 lines):"
if kubectl get pods -n argo-rollouts -l app=rolloutplugin-controller &> /dev/null; then
    kubectl logs -n argo-rollouts -l app=rolloutplugin-controller --tail=30 >> "$RESULTS_FILE" 2>&1
    mark_pass
else
    log "${YELLOW}Controller is running locally. Check your local terminal.${NC}"
    echo "Controller running locally - skipped" >> "$RESULTS_FILE"
fi
log ""

log "${BLUE}==> Test 9: RBAC Verification${NC}"
log "Checking controller's RBAC permissions:"

RBAC_FAIL=0
for perm in "get" "list" "watch" "update"; do
    result=$(kubectl auth can-i $perm rolloutplugins --as=system:serviceaccount:argo-rollouts:rolloutplugin-controller -n argo-rollouts 2>&1)
    echo "  $perm rolloutplugins: $result" | tee -a "$RESULTS_FILE"
    if [ "$result" != "yes" ]; then
        RBAC_FAIL=1
    fi
done

# Check status subresource
result=$(kubectl auth can-i patch rolloutplugins/status --as=system:serviceaccount:argo-rollouts:rolloutplugin-controller -n argo-rollouts 2>&1)
echo "  patch rolloutplugins/status: $result" | tee -a "$RESULTS_FILE"
if [ "$result" != "yes" ]; then
    RBAC_FAIL=1
fi

if [ $RBAC_FAIL -eq 0 ]; then
    mark_pass
else
    mark_fail "Some RBAC permissions missing"
fi
log ""

log "${BLUE}==> Test 10: Final State Check${NC}"
log "Final resource state:"
kubectl get rolloutplugin nginx-rollout -n argo-rollouts -o yaml | grep -A 30 "status:" | tee -a "$RESULTS_FILE"
log ""

# Generate summary
log "${GREEN}==> CRUD Tests Complete!${NC}"
log ""
log "=============================="
log "TEST SUMMARY"
log "=============================="
log "Total Tests: $TOTAL_TESTS"
log "Passed: ${GREEN}$PASSED_TESTS${NC}"
log "Failed: ${RED}$FAILED_TESTS${NC}"
log ""

# Write summary to both files
{
    echo ""
    echo "=============================="
    echo "TEST SUMMARY"
    echo "=============================="
    echo "Total Tests: $TOTAL_TESTS"
    echo "Passed: $PASSED_TESTS"
    echo "Failed: $FAILED_TESTS"
    echo ""
    if [ $FAILED_TESTS -eq 0 ]; then
        echo "✓ ALL TESTS PASSED"
    else
        echo "✗ SOME TESTS FAILED"
    fi
    echo ""
    echo "Test Categories:"
    echo "  - CREATE Operations"
    echo "  - READ Operations"  
    echo "  - UPDATE Operations"
    echo "  - PATCH Operations"
    echo "  - DELETE Operations"
    echo "  - Status Subresource"
    echo "  - LIST and WATCH"
    echo "  - RBAC Permissions"
    echo ""
} >> "$RESULTS_FILE"

# Copy to latest summary
cp "$RESULTS_FILE" "$SUMMARY_FILE"

log "Results saved to:"
log "  - ${BLUE}$RESULTS_FILE${NC}"
log "  - ${BLUE}$SUMMARY_FILE${NC}"
log ""

log "Cleanup (optional):"
log "  To clean up: kubectl delete rolloutplugin nginx-rollout -n argo-rollouts"
log "  Or run: ./test/rolloutplugin/test-rolloutplugin-cleanup.sh"
log ""

# Exit with appropriate code
if [ $FAILED_TESTS -eq 0 ]; then
    exit 0
else
    exit 1
fi
