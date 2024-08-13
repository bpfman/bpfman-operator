#!/usr/bin/env bash
#
# Load container images into a specified kind (Kubernetes IN Docker)
# cluster. Save images from either Docker or Podman, depending on the
# OCI_BIN environment variable. Defaults to Docker if OCI_BIN is not
# set.
#
# Usage:
#   ./kind-load-image.sh [-v] <KIND-CLUSTER-NAME> <IMAGE-REFERENCE> [<IMAGE-REFERENCE> ...]
#
# Arguments:
#   -v                 Verbose mode; echo commands before executing them.
#   KIND-CLUSTER-NAME  Specify the name of the kind cluster.
#   IMAGE-REFERENCE    Specify one or more image references
#                      (e.g., quay.io/bpfman/bpfman:latest).
#
# Example:
#   ./kind-load-image.sh -v my-cluster quay.io/bpfman/bpfman:latest quay.io/bpfman/bpfman-operator:latest
#
# The script:
#   1. Save each specified image as a tar file using the specified
#      OCI_BIN (Docker or Podman).
#   2. Load each tar file into the specified kind cluster.
#   3. Clean up temporary files created during the process.
#
# Ensure that the specified kind cluster is already running before
# executing the script.

set -o errexit
set -o nounset
set -o pipefail

# Parse options
verbose=0
if [ "$1" = "-v" ]; then
    verbose=1
    shift
fi

if [ $# -lt 2 ]; then
    echo "Usage: ${0##*/} [-v] <KIND_CLUSTER_NAME> <IMAGE REFERENCE> [<IMAGE REFERENCE> ...]"
    exit 1
fi

kind_cluster_name="$1"
shift

: "${OCI_BIN:=docker}"

tmp_dir=$(mktemp -d -t bpfman-XXXXXX)

cleanup() {
    rm -r "$tmp_dir"
}

trap cleanup EXIT

save_args=""
if [ "$OCI_BIN" = "podman" ]; then
    save_args="--quiet"
fi

counter=1
for image_reference in "$@"; do
    tar_file="$tmp_dir/image-$counter.tar"

    if [ $verbose -eq 1 ]; then
        echo "$OCI_BIN save $save_args --output \"$tar_file\" \"$image_reference\""
    fi
    "$OCI_BIN" save $save_args --output "$tar_file" "$image_reference"

    if [ $verbose -eq 1 ]; then
        echo "kind load image-archive --name \"$kind_cluster_name\" \"$tar_file\""
    fi
    kind load image-archive --name "$kind_cluster_name" "$tar_file"

    counter=$((counter + 1))
done
