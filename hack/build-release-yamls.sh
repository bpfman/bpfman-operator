#!/usr/bin/env bash

# Copyright 2022 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

thisyear=`date +"%Y"`

mkdir -p release-v${VERSION}/

## Location to install dependencies to
LOCALBIN=$(pwd)/bin

## Tool Binaries
KUSTOMIZE=${LOCALBIN}/kustomize

# Generate all install yaml's

## 1. bpfman CRD install

# Make clean files with boilerplate
cat hack/boilerplate.sh.txt > release-v${VERSION}/bpfman-crds-install.yaml
sed -i "s/YEAR/$thisyear/g" release-v${VERSION}/bpfman-crds-install.yaml
cat << EOF >> release-v${VERSION}/bpfman-crds-install.yaml
#
# bpfman Kubernetes API install
#
EOF

for file in `ls config/crd/bases/bpfman*.yaml`
do
    echo "---" >> release-v${VERSION}/bpfman-crds-install.yaml
    echo "#" >> release-v${VERSION}/bpfman-crds-install.yaml
    echo "# $file" >> release-v${VERSION}/bpfman-crds-install.yaml
    echo "#" >> release-v${VERSION}/bpfman-crds-install.yaml
    cat $file >> release-v${VERSION}/bpfman-crds-install.yaml
done

echo "Generated:" release-v${VERSION}/bpfman-crds-install.yaml

## 2.bpfman-operator install yaml

$(cd ./config/bpfman-operator-deployment && ${KUSTOMIZE} edit set image quay.io/bpfman/bpfman-operator=quay.io/bpfman/bpfman-operator:v${VERSION})
${KUSTOMIZE} build ./config/default > release-v${VERSION}/bpfman-operator-install.yaml
### replace configmap :latest images with :v${VERSION}
sed -i "s/quay.io\/bpfman\/bpfman-agent:latest/quay.io\/bpfman\/bpfman-agent:v${VERSION}/g" release-v${VERSION}/bpfman-operator-install.yaml
sed -i "s/quay.io\/bpfman\/bpfman:latest/quay.io\/bpfman\/bpfman:v${VERSION}/g" release-v${VERSION}/bpfman-operator-install.yaml

echo "Generated:" release-v${VERSION}/bpfman-operator-install.yaml
