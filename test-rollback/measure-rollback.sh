#!/bin/bash

LOG_FILE="test-rollback/logs/rollback_metrics-$(date +'%Y-%m-%d_%H-%M-%S').log"
ITERATIONS=5
ROLLOUT_NAME="example-rollout"
NAMESPACE="default"
MAX_REPLICAS=12  # Reduced from 15 to avoid resource constraints
MIN_REPLICAS=5
SCALE_UP_TIMEOUT=120  # 2 minutes timeout for scale up

echo "Starting rollback speed measurement with scale down" > $LOG_FILE
echo "Timestamp format: seconds.nanoseconds" >> $LOG_FILE
echo "Scenario: Testing rollback while scaling down from $MAX_REPLICAS to $MIN_REPLICAS replicas" >> $LOG_FILE

for i in $(seq 1 $ITERATIONS); do
    echo "=== Test iteration $i ===" >> $LOG_FILE
    
    # Scale up to max replicas first
    echo "Scaling up to $MAX_REPLICAS replicas..." >> $LOG_FILE
    kubectl patch rollout $ROLLOUT_NAME -n $NAMESPACE --type merge -p "{\"spec\":{\"replicas\":$MAX_REPLICAS}}"
    
    # Wait for scale up to complete with timeout
    SCALE_START=$(date +%s)
    while true; do
        CURRENT_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.replicas}')
        READY_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.readyReplicas}')
        AVAILABLE_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.availableReplicas}')
        
        # Check for pending pods
        PENDING_PODS=$(kubectl get pods -l app=example-app -n $NAMESPACE --field-selector=status.phase=Pending -o json | jq '.items | length')
        
        echo "Current replicas: $CURRENT_REPLICAS, Ready: $READY_REPLICAS, Available: $AVAILABLE_REPLICAS, Pending: $PENDING_PODS" >> $LOG_FILE
        
        # Check if timeout exceeded
        CURRENT=$(date +%s)
        ELAPSED=$((CURRENT - SCALE_START))
        if [ $ELAPSED -gt $SCALE_UP_TIMEOUT ]; then
            echo "Scale up timeout after ${ELAPSED}s. Using available replicas: $AVAILABLE_REPLICAS" >> $LOG_FILE
            # Use available replicas as the starting point instead
            MAX_REPLICAS=$AVAILABLE_REPLICAS
            break
        fi
        
        # Consider scale up complete if we have enough available replicas
        if [ ! -z "$AVAILABLE_REPLICAS" ] && [ "$AVAILABLE_REPLICAS" -ge "$MAX_REPLICAS" ]; then
            echo "Scale up complete with $AVAILABLE_REPLICAS available replicas" >> $LOG_FILE
            break
        fi
        
        sleep 2
    done
    
    # Record initial state
    INITIAL_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.replicas}')
    INITIAL_AVAILABLE=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.availableReplicas}')
    echo "Initial replica count: $INITIAL_REPLICAS (Available: $INITIAL_AVAILABLE)" >> $LOG_FILE
    
    # Update to trigger rollout
    kubectl argo rollouts set image $ROLLOUT_NAME \
        example-app=nginx:1.20.0 -n $NAMESPACE
    
    # Wait for rollout to start
    sleep 5
    
    # Trigger scale down simultaneously
    echo "Triggering scale down to $MIN_REPLICAS replicas..." >> $LOG_FILE
    kubectl patch rollout $ROLLOUT_NAME -n $NAMESPACE --type merge -p "{\"spec\":{\"replicas\":$MIN_REPLICAS}}"
    
    # Record start time for rollback
    START_TIME=$(date +%s.%N)
    echo "Rollback start time: $START_TIME" >> $LOG_FILE
    
    # Trigger rollback
    kubectl argo rollouts undo $ROLLOUT_NAME -n $NAMESPACE
    
    # Monitor until rollback completes while tracking replica count
    while true; do
        STATUS=$(kubectl argo rollouts status $ROLLOUT_NAME -n $NAMESPACE)
        CURRENT_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.replicas}')
        READY_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.readyReplicas}')
        AVAILABLE_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.availableReplicas}')
         
        # Log current state
        CURRENT_TIME=$(date +%s.%N)
        ELAPSED=$(echo "$CURRENT_TIME - $START_TIME" | bc)
        echo "T+${ELAPSED}s - Status: $STATUS, Replicas: $CURRENT_REPLICAS, Ready: $READY_REPLICAS, Available: $AVAILABLE_REPLICAS" >> $LOG_FILE
        
        if [ "$STATUS" == "Healthy" ]; then
            END_TIME=$(date +%s.%N)
            break
        fi
        sleep 2
    done
    
    # Calculate duration
    DURATION=$(echo "$END_TIME - $START_TIME" | bc)
    echo "Rollback end time: $END_TIME" >> $LOG_FILE
    echo "Rollback duration: $DURATION seconds" >> $LOG_FILE
    
    # Record final state
    FINAL_REPLICAS=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.replicas}')
    FINAL_READY=$(kubectl get rollout $ROLLOUT_NAME -n $NAMESPACE -o jsonpath='{.status.readyReplicas}')
    echo "Final replica count: $FINAL_REPLICAS (Ready: $FINAL_READY)" >> $LOG_FILE
    echo "Replicas scaled down: $(($INITIAL_REPLICAS - $FINAL_REPLICAS))" >> $LOG_FILE
    
    # Get prometheus metrics if available
    if kubectl get svc -n monitoring prometheus-server &>/dev/null; then
        echo "Prometheus metrics:" >> $LOG_FILE
        curl -s "http://localhost:9090/api/v1/query" \
            --data-urlencode 'query=rate(rollout_reconcile_duration_seconds_sum{phase="rollback"}[1m])' \
            >> $LOG_FILE
    fi
    
    # Wait between iterations for stabilization
    echo "Waiting for cooldown..." >> $LOG_FILE
    sleep 60
done

# Calculate statistics
echo "=== Summary ===" >> $LOG_FILE
AVG=$(awk '/Rollback duration/ {sum+=$3; count++} END {print sum/count}' $LOG_FILE)
MIN=$(awk '/Rollback duration/ {print $3}' $LOG_FILE | sort -n | head -1)
MAX=$(awk '/Rollback duration/ {print $3}' $LOG_FILE | sort -n | tail -1)

echo "Average rollback duration: $AVG seconds" >> $LOG_FILE
echo "Min duration: $MIN seconds" >> $LOG_FILE
echo "Max duration: $MAX seconds" >> $LOG_FILE

# Calculate average replicas scaled down
AVG_SCALED_DOWN=$(awk '/Replicas scaled down/ {sum+=$4; count++} END {if(count>0) print sum/count; else print 0}' $LOG_FILE)
echo "Average replicas scaled down during rollback: $AVG_SCALED_DOWN" >> $LOG_FILE

# Also print to console
echo "=== Rollback Performance Summary (Scale Down Scenario) ==="
echo "Iterations: $ITERATIONS"
echo "Average rollback duration: $AVG seconds"
echo "Min: $MIN seconds"
echo "Max: $MAX seconds"
echo "Average replicas scaled down: $AVG_SCALED_DOWN"

# Print replica progression for each iteration
echo ""
echo "=== Replica Count Progression ==="
grep "Initial replica count\|Final replica count\|Replicas scaled down" $LOG_FILE