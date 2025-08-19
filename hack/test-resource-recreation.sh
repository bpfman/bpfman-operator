#!/usr/bin/env bash

# test-resource-recreation.sh - Test Config owned resource recreation
# This script tests that deleting Config-owned resources triggers
# automatic recreation by the operator via owner reference protection.

set -e

echo "=== bpfman-operator Config Resource Recreation Test ==="
echo

failures=0

# Function to capture resource metadata before deletion
capture_resource_metadata() {
    local resource_type="$1"
    local resource_name="$2"
    local namespace="$3"
    
    local kubectl_cmd="kubectl get $resource_type $resource_name -o jsonpath='{.metadata.uid}'"
    if [ -n "$namespace" ]; then
        kubectl_cmd="kubectl get $resource_type $resource_name -n $namespace -o jsonpath='{.metadata.uid}'"
    fi
    
    eval "$kubectl_cmd" 2>/dev/null || echo ""
}

# Function to wait for resource recreation
wait_for_recreation() {
    local resource_type="$1"
    local resource_name="$2"
    local namespace="$3"
    local old_uid="$4"
    local timeout="${5:-30}"
    
    local kubectl_cmd="kubectl get $resource_type $resource_name -o jsonpath='{.metadata.uid}'"
    if [ -n "$namespace" ]; then
        kubectl_cmd="kubectl get $resource_type $resource_name -n $namespace -o jsonpath='{.metadata.uid}'"
    fi
    
    echo -n "Waiting for $resource_type/$resource_name to be recreated... "
    local count=0
    while true; do
        local current_uid
        current_uid=$(eval "$kubectl_cmd" 2>/dev/null || echo "")
        
        # Resource exists and has different UID = recreated
        if [[ -n "$current_uid" && "$current_uid" != "$old_uid" ]]; then
            echo "RECREATED (${count}s, new UID: ${current_uid:0:8}...)"
            return 0
        fi
        
        sleep 1
        ((count++))
        if [ $count -ge "$timeout" ]; then
            echo "TIMEOUT after ${timeout}s"
            return 1
        fi
    done
}

echo "=== Step 1: Capture Current Resource Metadata ==="
echo

# Capture UIDs for comparison after recreation
old_configmap_uid=$(capture_resource_metadata "configmap" "bpfman-config" "bpfman")
old_daemon_uid=$(capture_resource_metadata "daemonset" "bpfman-daemon" "bpfman")
old_metrics_uid=$(capture_resource_metadata "daemonset" "bpfman-metrics-proxy" "bpfman")
old_csi_uid=$(capture_resource_metadata "csidriver" "csi.bpfman.io" "")

echo "Current resource UIDs:"
echo "- ConfigMap: ${old_configmap_uid:0:8}..."
echo "- DaemonSet bpfman-daemon: ${old_daemon_uid:0:8}..."
echo "- DaemonSet bpfman-metrics-proxy: ${old_metrics_uid:0:8}..."
echo "- CSIDriver: ${old_csi_uid:0:8}..."

echo
echo "=== Step 2: Delete Resources One by One ==="
echo

# Test ConfigMap recreation
echo "Deleting ConfigMap bpfman-config..."
kubectl delete configmap bpfman-config -n bpfman
wait_for_recreation "configmap" "bpfman-config" "bpfman" "$old_configmap_uid" 30 || ((failures++))

# Test DaemonSet bpfman-daemon recreation
echo "Deleting DaemonSet bpfman-daemon..."
kubectl delete daemonset bpfman-daemon -n bpfman
wait_for_recreation "daemonset" "bpfman-daemon" "bpfman" "$old_daemon_uid" 60 || ((failures++))

# Test DaemonSet bpfman-metrics-proxy recreation
echo "Deleting DaemonSet bpfman-metrics-proxy..."
kubectl delete daemonset bpfman-metrics-proxy -n bpfman
wait_for_recreation "daemonset" "bpfman-metrics-proxy" "bpfman" "$old_metrics_uid" 60 || ((failures++))

# Test CSIDriver recreation
echo "Deleting CSIDriver csi.bpfman.io..."
kubectl delete csidriver csi.bpfman.io
wait_for_recreation "csidriver" "csi.bpfman.io" "" "$old_csi_uid" 30 || ((failures++))

echo
echo "=== Step 3: Verify All Resources Are Healthy ==="
echo

# Run the verification script if available
if [ -f "./hack/verify-deployment.sh" ]; then
    echo "Running verification script..."
    if ./hack/verify-deployment.sh; then
        echo "Verification script: PASS"
    else
        echo "Verification script: FAIL"
        ((failures++))
    fi
else
    echo "Verification script not found, skipping final verification"
fi

echo
echo "=== Resource Recreation Test Complete ==="
echo

if [[ $failures -eq 0 ]]; then
    echo "PASS All owned resources successfully recreated!"
    echo "This confirms that:"
    echo "- Config controller properly recreates deleted owned resources"
    echo "- Owner reference protection is working correctly"
    echo "- All recreated resources are healthy and functional"
    exit 0
else
    echo "FAIL $failures recreation test(s) failed!"
    echo "Check the output above for details."
    exit 1
fi