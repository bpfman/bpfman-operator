#!/usr/bin/env bash

set -eu

echo "=== bpfman-operator Config CRD Deployment Verification ==="
echo

# Function to check if a resource exists
check_resource() {
    local resource_type="$1"
    local resource_name="$2"
    local namespace="$3"
    
    local kubectl_cmd="kubectl get $resource_type $resource_name"
    if [ -n "$namespace" ]; then
        kubectl_cmd="$kubectl_cmd -n $namespace"
    fi
    
    if eval "$kubectl_cmd" >/dev/null 2>&1; then
        echo "✓ $resource_type/$resource_name exists"
        return 0
    else
        echo "✗ $resource_type/$resource_name missing"
        return 1
    fi
}

failures=0

echo "=== Checking Config CRD and Resource ==="
check_resource "crd" "configs.bpfman.io" "" || ((failures++))
check_resource "config" "bpfman-config" "" || ((failures++))

echo
echo "=== Checking Owned Resources ==="
check_resource "configmap" "bpfman-config" "bpfman" || ((failures++))
check_resource "daemonset" "bpfman-daemon" "bpfman" || ((failures++))
check_resource "daemonset" "bpfman-metrics-proxy" "bpfman" || ((failures++))
check_resource "csidriver" "csi.bpfman.io" "" || ((failures++))

echo
echo "=== Checking Operator ==="
check_resource "deployment" "bpfman-operator" "bpfman" || ((failures++))

echo
if [[ $failures -eq 0 ]]; then
    echo "✓ All Config CRD deployment verification checks passed!"
    exit 0
else
    echo "✗ $failures verification check(s) failed!"
    exit 1
fi