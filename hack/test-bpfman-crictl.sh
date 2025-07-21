#!/usr/bin/env bash

# Self-test tool for the bpfman-crictl implementation. bpfman-crictl
# is a cutdown version of crictl that focuses on extracting container
# PIDs needed for eBPF program attachment.
#
# This script allows you to run the /bpfman-crictl tool against a pod
# and container to extract the container's PID.
#
# USAGE EXAMPLES
# --------------
#
# 0. Self-test (no arguments) - inspect bpfman-agent container itself:
#    ./hack/test-bpfman-crictl.sh
#
# 1. Basic usage - inspect containers in specific namespace with labels:
#    ./test-bfpman-crictl.sh --namespace go-target --labels name=go-target
#
# 2. Inspect specific pod and container:
#    ./test-bfpman-crictl.sh --pod-name go-target-ds-sfbb9 --container-name go-target
#
# 3. Inspect containers in a different namespace:
#    ./test-bfpman-crictl.sh --namespace go-uprobe-counter --labels name=go-uprobe-counter
#
# 4. Search across all namespaces with specific labels:
#    ./test-bfpman-crictl.sh --labels app=example-app
#
# CONFIGURATION
# -------------
# The script supports command-line arguments for configuration:
#
# Bpfman Configuration:
# --bpfman-namespace: Namespace containing bpfman-agent (default: bpfman)
# --bpfman-pod-selector: Label selector for bpfman-agent pod (default: name=bpfman-daemon)
# --bpfman-container: Container name within bpfman-agent pod (default: bpfman-agent)
# --crictl-binary: Path to crictl binary in container (default: /bpfman-crictl)
#
# Target Discovery:
# --namespace: Specific namespace to search (optional)
# --labels: Kubernetes label selector (e.g., 'app=web,env=prod')
# --pod-name: Specific pod name to inspect (optional)
# --container-name: Specific container name within pod (optional)
#
# TYPICAL WORKFLOW
# ----------------
# 1. Script validates prerequisites (kubectl, jq, cluster connectivity)
# 2. Discovers bpfman-agent pod using provided selectors
# 3. Verifies bpfman-crictl binary is available in the agent container
# 4. Finds target pods based on namespace and label selectors
# 5. For each target pod:
#    a. Retrieves pod information using bpfman-crictl
#    b. Lists all containers within the pod
#    c. Filters containers by name if specified
#    d. Inspects each container to extract PID and metadata
# 6. Displays comprehensive results with colour-coded output
#
# VERIFYING PID NUMBERS ON KIND CONTROL PLANE
# -------------------------------------------
#
# Since KIND runs containers within Docker, the PIDs returned by this
# script are from the container runtime's perspective. To verify these
# PIDs match actual processes on the KIND control plane (which runs in
# Docker):
#
# 1. Get the KIND control plane container name:
#    docker ps | grep control-plane
#    # Example output: bpfman-operator-control-plane
#
# 2. Execute into the KIND control plane container:
#    docker exec -it bpfman-operator-control-plane bash
#
# 3. Inside the control plane, check if the PID exists:
#    ps aux | grep <PID>
#
# 4. Alternatively, verify from the host using Docker:
#    docker exec bpfman-operator-control-plane ps aux | grep <PID>
#
# Example verification workflow:
# - Script returns PID 4025 for go-target container
# - Run: docker exec bpfman-operator-control-plane ps aux | grep 4025
# - Should show the go-target process running with that PID

set -eu

bpfman_namespace="bpfman"
bpfman_pod_selector="name=bpfman-daemon"
bpfman_container="bpfman-agent"
crictl_binary="/bpfman-crictl"

namespace=""
pod_labels=""
pod_name=""
container_name=""

log_info() {
    echo "[INFO] $1"
}

log_success() {
    echo "[SUCCESS] $1"
}

log_warning() {
    echo "[WARNING] $1"
}

log_error() {
    echo "[ERROR] $1"
}

show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Use bpfman-crictl from bpfman-agent pod to inspect containers in other namespaces"
    echo ""
    echo "Options:"
    echo "  --bpfman-namespace NAMESPACE      Namespace containing bpfman-agent (default: bpfman)"
    echo "  --bpfman-pod-selector SELECTOR    Pod selector for bpfman-agent pod (default: name=bpfman-daemon)"
    echo "  --bpfman-container CONTAINER      Container name for bpfman-agent (default: bpfman-agent)"
    echo "  --crictl-binary BINARY            Path to crictl binary (default: /bpfman-crictl)"
    echo ""
    echo "  --namespace NAMESPACE             Target namespace to inspect containers from"
    echo "  --labels LABELS                   Target pod labels (e.g., 'app=myapp,env=prod')"
    echo "  --pod-name NAME                   Specific target pod name"
    echo "  --container-name NAME             Specific target container name"
    echo ""
    echo "  -h, --help                        Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 --namespace go-target --labels name=go-target"
    echo "  $0 --pod-name go-target-ds-sfbb9 --container-name go-target"
    echo "  $0 --namespace go-uprobe-counter --labels name=go-uprobe-counter"
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --bpfman-namespace)
            bpfman_namespace="$2"
            shift 2
            ;;
        --bpfman-pod-selector)
            bpfman_pod_selector="$2"
            shift 2
            ;;
        --bpfman-container)
            bpfman_container="$2"
            shift 2
            ;;
        --crictl-binary)
            crictl_binary="$2"
            shift 2
            ;;
        --namespace)
            namespace="$2"
            shift 2
            ;;
        --labels)
            pod_labels="$2"
            shift 2
            ;;
        --pod-name)
            pod_name="$2"
            shift 2
            ;;
        --container-name)
            container_name="$2"
            shift 2
            ;;
        -h|--help)
            show_usage
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            show_usage
            exit 1
            ;;
    esac
done

check_prerequisites() {
    log_info "Checking prerequisites..."

    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi

    if ! command -v jq &> /dev/null; then
        log_error "jq is not installed or not in PATH"
        exit 1
    fi

    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi

    log_success "Prerequisites check passed"
}

discover_bpfman_pod() {
    log_info "Discovering bpfman-agent pod..."
    log_info "Namespace: $bpfman_namespace"
    log_info "Selector: $bpfman_pod_selector"
    log_info "Container: $bpfman_container"

    if ! kubectl get namespace "$bpfman_namespace" &> /dev/null; then
        log_error "Namespace '$bpfman_namespace' does not exist"
        exit 1
    fi

    local bpfman_pod_result
    bpfman_pod_result=$(kubectl get pods -n "$bpfman_namespace" -l "$bpfman_pod_selector" --no-headers -o custom-columns=":metadata.name" | head -1)
    bpfman_pod_name="$bpfman_pod_result"
    if [ -z "$bpfman_pod_name" ]; then
        log_error "No bpfman-agent pods found with selector '$bpfman_pod_selector' in namespace '$bpfman_namespace'"
        exit 1
    fi

    if ! kubectl get pod "$bpfman_pod_name" -n "$bpfman_namespace" -o jsonpath='{.spec.containers[*].name}' | grep -q "$bpfman_container"; then
        log_error "Container '$bpfman_container' not found in pod '$bpfman_pod_name'"
        exit 1
    fi

    log_success "Found bpfman-agent pod: $bpfman_pod_name"
}

test_crictl_binary() {
    log_info "Testing crictl binary availability..."

    if ! kubectl exec -n "$bpfman_namespace" "$bpfman_pod_name" -c "$bpfman_container" -- test -f "$crictl_binary" 2>/dev/null; then
        log_error "Binary '$crictl_binary' not found in container '$bpfman_container'"
        exit 1
    fi

    log_success "Binary '$crictl_binary' found and accessible"
}

discover_target_pods() {
    log_info "Discovering target pods..."

    if [ -n "$pod_name" ]; then
        log_info "Using specific target pod: $pod_name"
        target_pods="$pod_name"
        return
    fi

    # Default: use bpfman-agent pod itself for self-test
    if [ -z "$namespace" ] && [ -z "$pod_labels" ]; then
        log_info "No target specified, using bpfman-agent pod for self-test"
        target_pods="$bpfman_pod_name"
        return
    fi

    local kubectl_cmd="kubectl get pods"

    if [ -n "$namespace" ]; then
        kubectl_cmd="$kubectl_cmd -n $namespace"
    else
        kubectl_cmd="$kubectl_cmd -A"
    fi

    if [ -n "$pod_labels" ]; then
        kubectl_cmd="$kubectl_cmd -l $pod_labels"
    fi

    kubectl_cmd="$kubectl_cmd --no-headers -o custom-columns=:metadata.name"

    log_info "Discovering pods with command: $kubectl_cmd"

    local target_pods_result
    target_pods_result=$(eval "$kubectl_cmd")
    target_pods="$target_pods_result"
    if [ -z "$target_pods" ]; then
        log_error "No target pods found"
        exit 1
    fi

    log_success "Found target pods:"
    echo "$target_pods" | while read -r pod; do echo "  $pod"; done
}

exec_crictl() {
    local cmd="$1"
    local description="$2"

    log_info "$description"
    echo "Command: $crictl_binary $cmd"
    echo

    # shellcheck disable=SC2086
    if ! kubectl exec -n "$bpfman_namespace" "$bpfman_pod_name" -c "$bpfman_container" -- "$crictl_binary" $cmd 2>/dev/null; then
        log_error "Failed to execute: $crictl_binary $cmd"
        return 1
    fi

    echo
    return 0
}

inspect_target_containers() {
    log_info "=== Starting container inspection ==="

    for pod_name in $target_pods; do
        log_info "Processing pod: $pod_name"

        if ! exec_crictl "pods --name $pod_name -o json" "1. Getting pod info for $pod_name"; then
            log_warning "Failed to get pod info for $pod_name, skipping..."
            continue
        fi

        local pod_id pod_info
        pod_info=$(kubectl exec -n "$bpfman_namespace" "$bpfman_pod_name" -c "$bpfman_container" -- "$crictl_binary" pods --name "$pod_name" -o json 2>/dev/null)
        pod_id=$(echo "$pod_info" | jq -r '.items[0].id')
        if [ -z "$pod_id" ] || [ "$pod_id" = "null" ]; then
            log_warning "Could not extract pod ID for $pod_name, skipping..."
            continue
        fi

        log_success "Pod ID: $pod_id"

        if ! exec_crictl "ps --pod $pod_id -o json" "2. Listing containers in pod $pod_name"; then
            log_warning "Failed to list containers for pod $pod_name, skipping..."
            continue
        fi

        local container_ids containers_info
        containers_info=$(kubectl exec -n "$bpfman_namespace" "$bpfman_pod_name" -c "$bpfman_container" -- "$crictl_binary" ps --pod "$pod_id" -o json 2>/dev/null)
        container_ids=$(echo "$containers_info" | jq -r '.containers[].id')

        if [ -z "$container_ids" ]; then
            log_warning "No containers found in pod $pod_name"
            continue
        fi

        if [ -n "$container_name" ]; then
            local filtered_container_ids
            filtered_container_ids=$(echo "$containers_info" | jq -r ".containers[] | select(.metadata.name == \"$container_name\") | .id")
            if [ -z "$filtered_container_ids" ]; then
                log_warning "Container '$container_name' not found in pod $pod_name"
                continue
            fi
            container_ids="$filtered_container_ids"
        fi

        for container_id in $container_ids; do
            log_info "Inspecting container: $container_id"

            if ! exec_crictl "inspect -o json $container_id" "3. Inspecting container $container_id"; then
                log_warning "Failed to inspect container $container_id, skipping..."
                continue
            fi

            local container_info container_pid container_name_result
            container_info=$(kubectl exec -n "$bpfman_namespace" "$bpfman_pod_name" -c "$bpfman_container" -- "$crictl_binary" inspect -o json "$container_id" 2>/dev/null)
            container_pid=$(echo "$container_info" | jq -r '.info.pid')
            container_name_result=$(echo "$container_info" | jq -r '.status.metadata.name')

            log_success "Container: $container_name_result (ID: $container_id) - PID: $container_pid"
        done

        echo
    done
}

main() {
    check_prerequisites
    discover_bpfman_pod
    test_crictl_binary
    discover_target_pods
    inspect_target_containers
}

main "$@"
