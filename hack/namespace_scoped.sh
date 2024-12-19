#!/usr/bin/env bash

set -o errexit

# This script generates the contents for a kubeconfig file for a given ServiceAccount. 

# Default the control variables, but they can be overwritten by passing in environment variables.
CLUSTER_NAME=${CLUSTER_NAME:-bpfman-deployment}
NAMESPACE=${NAMESPACE:-acme}
SERVICE_ACCOUNT=${SERVICE_ACCOUNT:-test-account}
SECRET_NAME=${SECRET_NAME:-test-account-token}

# If a file is passed in, try to initialize the control variables from data in the file.
if [ ! -z "$1" ] ; then
  if [ -f $1 ]; then
    # For each variable below, pull the `kind: Secret` object from the file, which are the contents
    # between "kind: Secret" and a possible "---". Then pipe that output into a `grep` to find
    # individual fields. Then us `awk` to pull put the value.

    # Get NAMESPACE
    tmpVar=$(awk '/kind: Secret/,/---/' $1 | grep -m 1 " namespace:" | awk -F' ' '{print $2}')
    if [ ! -z "$tmpVar" ] ; then
      NAMESPACE=${tmpVar}
    fi

    # Get SERVICE_ACCOUNT
    tmpVar=$(awk '/kind: Secret/,/---/' $1 | grep -m 1 " kubernetes.io/service-account.name:" | awk -F' ' '{print $2}')
    if [ ! -z "$tmpVar" ] ; then
      SERVICE_ACCOUNT=${tmpVar}
    fi

    # Get SECRET_NAME
    tmpVar=$(awk '/kind: Secret/,/---/' $1 | grep -m 1 " name:" | awk -F' ' '{print $2}')
    if [ ! -z "$tmpVar" ] ; then
      SECRET_NAME=${tmpVar}
    fi
  fi
fi

# Determine the server IP Address using `kubectl cluster-info`.
# The first line of the output looks like:
#   "Kubernetes control plane is running at https://127.0.0.1:35841"
# The `grep` command gets the first instance of the "https" and returns that line.
# The 'awk' command pulls out the 7th word in the sentence, the  "https:" part.
# The `sed` command strips off some color control characters that are include in the output.
server=$(kubectl cluster-info | grep -m 1 "https"  | awk -F' ' '{print $7}' | sed 's/\x1b\[[0-9;]*m//g')

# Get the secret data.
ca=$(kubectl --namespace="${NAMESPACE}" get secret/"${SECRET_NAME}" -o=jsonpath='{.data.ca\.crt}')
token=$(kubectl --namespace="${NAMESPACE}" get secret/"${SECRET_NAME}" -o=jsonpath='{.data.token}' | base64 --decode)

# Generate a kubeconfig based on gathered data.
echo "apiVersion: v1
kind: Config
clusters:
  - name: ${CLUSTER_NAME}
    cluster:
      certificate-authority-data: ${ca}
      server: ${server}
contexts:
  - name: ${SERVICE_ACCOUNT}@${CLUSTER_NAME}
    context:
      cluster: ${CLUSTER_NAME}
      namespace: ${NAMESPACE}
      user: ${SERVICE_ACCOUNT}
users:
  - name: ${SERVICE_ACCOUNT}
    user:
      token: ${token}
current-context: ${SERVICE_ACCOUNT}@${CLUSTER_NAME}
"
