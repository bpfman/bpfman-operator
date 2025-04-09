#!/bin/bash

# Number of test pods to launch (default: 5)
NUM_PODS=${1:-5}
NAMESPACE="default"
IMAGE="busybox"
LABEL="app=test-pod"

echo "Creating $NUM_PODS test pods in namespace '$NAMESPACE'..."

for i in $(seq 1 $NUM_PODS); do
  POD_NAME="test-pod-$i"
  echo "Creating pod: $POD_NAME"
  kubectl run $POD_NAME \
    --image=$IMAGE \
    --restart=Never \
    --labels="$LABEL" \
    --command -- sleep 3600
done

echo "Done. Use 'kubectl get pods -l $LABEL' to see the pods."
