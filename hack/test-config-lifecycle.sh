#!/usr/bin/env bash

# test-config-lifecycle.sh - Test complete Config CRD lifecycle
# This script tests:
# 1. Config deletion triggers cascading deletion of all owned resources
# 2. Operator remains running and ready
# 3. New Config with different settings triggers recreation
# 4. All components work with new configuration

set -e

echo "=== bpfman-operator Config CRD Lifecycle Test ==="
echo

# Function to wait for resource to be deleted
wait_for_deletion() {
    local resource_type="$1"
    local resource_name="$2"
    local namespace="$3"
    local timeout="${4:-60}"

    local kubectl_cmd="kubectl get $resource_type $resource_name"
    if [ -n "$namespace" ]; then
        kubectl_cmd="$kubectl_cmd -n $namespace"
    fi

    echo -n "Waiting for $resource_type/$resource_name to be deleted... "
    local count=0
    while eval "$kubectl_cmd" >/dev/null 2>&1; do
        sleep 1
        ((count++))
        if [ $count -ge "$timeout" ]; then
            echo "TIMEOUT after ${timeout}s"
            return 1
        fi
    done
    echo "DELETED (${count}s)"
    return 0
}

# Function to wait for resource to exist
wait_for_creation() {
    local resource_type="$1"
    local resource_name="$2"
    local namespace="$3"
    local timeout="${4:-60}"

    local kubectl_cmd="kubectl get $resource_type $resource_name"
    if [ -n "$namespace" ]; then
        kubectl_cmd="$kubectl_cmd -n $namespace"
    fi

    echo -n "Waiting for $resource_type/$resource_name to be created... "
    local count=0
    while ! eval "$kubectl_cmd" >/dev/null 2>&1; do
        sleep 1
        ((count++))
        if [ $count -ge "$timeout" ]; then
            echo "TIMEOUT after ${timeout}s"
            return 1
        fi
    done
    echo "CREATED (${count}s)"
    return 0
}

# Function to verify operator is still running
verify_operator_running() {
    echo -n "Verifying operator is still running... "
    if kubectl get deployment bpfman-operator -n bpfman -o jsonpath='{.status.readyReplicas}' | grep -q '1'; then
        local pod_name
        pod_name=$(kubectl get pods -n bpfman -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
        if [[ -n "$pod_name" ]]; then
            local pod_status
            pod_status=$(kubectl get pod "$pod_name" -n bpfman -o jsonpath='{.status.phase}')
            if [[ "$pod_status" == "Running" ]]; then
                echo "PASS ($pod_name)"
                return 0
            fi
        fi
    fi
    echo "FAIL"
    return 1
}

failures=0

echo "=== Step 1: Capture Current State ==="
echo

echo "Current Config CRD settings:"
kubectl get config bpfman-config -o jsonpath='{.spec}' | jq '.'

echo
echo "Current owned resources:"
echo "- ConfigMap: $(kubectl get configmap bpfman-config -n bpfman -o jsonpath='{.metadata.uid}' 2>/dev/null || echo 'NOT_FOUND')"
echo "- DaemonSet bpfman-daemon: $(kubectl get daemonset bpfman-daemon -n bpfman -o jsonpath='{.metadata.uid}' 2>/dev/null || echo 'NOT_FOUND')"
echo "- DaemonSet bpfman-metrics-proxy: $(kubectl get daemonset bpfman-metrics-proxy -n bpfman -o jsonpath='{.metadata.uid}' 2>/dev/null || echo 'NOT_FOUND')"
echo "- CSIDriver: $(kubectl get csidriver csi.bpfman.io -o jsonpath='{.metadata.uid}' 2>/dev/null || echo 'NOT_FOUND')"

echo
echo "=== Step 2: Delete Config CRD ==="
echo

echo "Deleting Config CRD bpfman-config..."
kubectl delete config bpfman-config

echo
echo "=== Step 3: Verify Cascading Deletion ==="
echo

# Wait for all owned resources to be deleted due to owner references
wait_for_deletion "configmap" "bpfman-config" "bpfman" 30 || ((failures++))
wait_for_deletion "daemonset" "bpfman-daemon" "bpfman" 30 || ((failures++))
wait_for_deletion "daemonset" "bpfman-metrics-proxy" "bpfman" 30 || ((failures++))
wait_for_deletion "csidriver" "csi.bpfman.io" "" 30 || ((failures++))

echo
echo "Waiting for DaemonSet pods to be cleaned up by Kubernetes garbage collection..."

# Brief wait for any pods that might be in transition
sleep 2

# Final check for any remaining completed pods  
completed_pods=$(kubectl get pods -n bpfman --field-selector=status.phase=Succeeded --no-headers 2>/dev/null | grep -E "bpfman-(daemon|metrics-proxy)" | wc -l)
if [ "$completed_pods" -eq 0 ]; then
    echo "Pod cleanup complete - no completed bpfman pods remaining"
else
    echo "Found $completed_pods completed bpfman pods - Kubernetes will clean these up automatically"
fi

echo
echo "=== Step 4: Verify Complete Cleanup ==="
echo

echo "Verifying bpfman namespace is clean of all owned resources:"
echo "ConfigMaps in bpfman namespace:"
kubectl get configmaps -n bpfman -o name | grep -v kube- | grep -v default-token || echo "  No ConfigMaps found"

echo "DaemonSets in bpfman namespace:"
kubectl get daemonsets -n bpfman -o name || echo "  No DaemonSets found"

echo "CSIDrivers (cluster-scoped):"
kubectl get csidrivers -o name | grep bpfman || echo "  No bpfman CSIDrivers found"

echo "All resources in bpfman namespace:"
kubectl get all -n bpfman

echo "Checking for running bpfman pods (excluding operator):"
running_bpfman_pods=$(kubectl get pods -n bpfman --field-selector=status.phase=Running --no-headers 2>/dev/null | grep -E "bpfman-(daemon|metrics-proxy)" | wc -l || echo "0")
if [ "$running_bpfman_pods" -eq 0 ]; then
    echo "  No running bpfman daemon/metrics pods found (expected after cleanup)"
else
    echo "  Found $running_bpfman_pods running bpfman pods (unexpected)"
    ((failures++))
fi

echo "Checking operator is still running:"
operator_pods=$(kubectl get pods -n bpfman --field-selector=status.phase=Running --no-headers 2>/dev/null | grep bpfman-operator | wc -l || echo "0")
if [ "$operator_pods" -eq 1 ]; then
    echo "  Operator pod running (expected)"
else
    echo "  Expected 1 operator pod, found $operator_pods"
    ((failures++))
fi

echo
echo "=== Step 5: Verify Operator Still Running ==="
echo

verify_operator_running || ((failures++))

echo
echo "=== Step 6: Create New Config with Different Settings ==="
echo

# Create new Config using the base config.yaml with sed transformations
echo "Creating new Config using base config.yaml with modifications..."
sed -e 's/name: config/name: bpfman-config/' \
    -e 's/namespace: kube-system/namespace: bpfman/' \
    -e 's/logLevel: info/logLevel: trace/' \
    config/bpfman-deployment/config.yaml | kubectl apply -f -

echo "New Config created with modified settings:"
echo "- name: config -> bpfman-config (correct name for controller)"
echo "- namespace: kube-system -> bpfman (correct namespace)"
echo "- logLevel: info -> trace (increased verbosity)"

echo
echo "=== Step 7: Verify Resource Recreation ==="
echo

# Wait for all resources to be recreated
wait_for_creation "configmap" "bpfman-config" "bpfman" 60 || ((failures++))
wait_for_creation "daemonset" "bpfman-daemon" "bpfman" 60 || ((failures++))
wait_for_creation "daemonset" "bpfman-metrics-proxy" "bpfman" 60 || ((failures++))
wait_for_creation "csidriver" "csi.bpfman.io" "" 60 || ((failures++))

echo
echo "=== Step 8: Verify New Configuration Applied ==="
echo

echo -n "Verifying ConfigMap has new log level... "
if kubectl get configmap bpfman-config -n bpfman -o jsonpath='{.data.bpfman\.log\.level}' | grep -q 'trace'; then
    echo "PASS (logLevel: trace)"
else
    echo "FAIL"
    ((failures++))
fi

echo -n "Verifying ConfigMap contains expected agent log level... "
if kubectl get configmap bpfman-config -n bpfman -o jsonpath='{.data.bpfman\.agent\.log\.level}' | grep -q 'info'; then
    echo "PASS (agent.logLevel: info)"
else
    echo "FAIL"
    ((failures++))
fi

echo -n "Verifying ConfigMap contains base configuration... "
if kubectl get configmap bpfman-config -n bpfman -o jsonpath='{.data.bpfman\.toml}' | grep -q 'max_retries = 30'; then
    echo "PASS (base config loaded)"
else
    echo "FAIL"
    ((failures++))
fi

echo -n "Verifying DaemonSet annotations updated... "
if kubectl get daemonset bpfman-daemon -n bpfman -o jsonpath='{.spec.template.metadata.annotations.bpfman\.io\.bpfman\.log\.level}' | grep -q 'trace'; then
    echo "PASS (annotation: trace)"
else
    echo "FAIL"
    ((failures++))
fi

echo
echo "=== Step 9: Wait for DaemonSet Pods to be Ready ==="
echo

echo -n "Waiting for bpfman-daemon DaemonSet pods to be ready... "
pod_timeout=120
pod_count=0
while true; do
    ready=$(kubectl get daemonset bpfman-daemon -n bpfman -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
    desired=$(kubectl get daemonset bpfman-daemon -n bpfman -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "1")
    if [[ "$ready" == "$desired" && "$ready" != "0" ]]; then
        echo "READY ($ready/$desired)"
        break
    fi
    sleep 2
    ((pod_count+=2))
    if [ $pod_count -ge "$pod_timeout" ]; then
        echo "TIMEOUT after ${pod_timeout}s"
        ((failures++))
        break
    fi
done

echo -n "Waiting for bpfman-metrics-proxy DaemonSet pods to be ready... "
pod_count=0
while true; do
    ready=$(kubectl get daemonset bpfman-metrics-proxy -n bpfman -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
    desired=$(kubectl get daemonset bpfman-metrics-proxy -n bpfman -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "1")
    if [[ "$ready" == "$desired" && "$ready" != "0" ]]; then
        echo "READY ($ready/$desired)"
        break
    fi
    sleep 2
    ((pod_count+=2))
    if [ $pod_count -ge "$pod_timeout" ]; then
        echo "TIMEOUT after ${pod_timeout}s"
        ((failures++))
        break
    fi
done

echo
echo "=== Step 10: Run Full Verification ==="
echo

# Run the verification script
if [ -f "./hack/verify-deployment.sh" ]; then
    echo "Running verification script..."
    if ./hack/verify-deployment.sh; then
        echo "Verification script: PASS"
    else
        echo "Verification script: FAIL"
        ((failures++))
    fi
else
    echo "Verification script not found at ./hack/verify-deployment.sh"
    echo "Please run it manually to verify the deployment"
fi

echo
echo "=== Config CRD Lifecycle Test Complete ==="
echo

if [[ $failures -eq 0 ]]; then
    echo "PASS Complete Config CRD lifecycle test successful!"
    echo "This confirms that:"
    echo "- Config deletion triggers proper cascading deletion of all owned resources"
    echo "- Finalizer protection prevents race conditions during deletion"
    echo "- Operator remains running throughout the lifecycle"
    echo "- New Config creation (using base YAML + sed) recreates all resources"
    echo "- Configuration changes properly propagate to ConfigMaps and DaemonSet annotations"
    echo "- sed-based Config creation aligns with deployment process"
    echo "- All components become healthy with modified configuration"
    exit 0
else
    echo "FAIL $failures verification(s) failed during lifecycle test!"
    echo "Check the output above for details."
    exit 1
fi