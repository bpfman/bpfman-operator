#!/usr/bin/env bash
set -eu

#!/usr/bin/env bash

export BPFMAN_OPERATOR_IMAGE_PULLSPEC="registry.redhat.io/bpfman/bpfman-rhel9-operator@sha256:3cf6df025d2814ad9da7951b528c19bf10bcef955390f61c0b1a4473dde587d1"

export CSV_FILE=/manifests/bpfman-operator.clusterserviceversion.yaml

sed -i -e "s|quay.io/bpfman/bpfman-operator:v.*|\"${BPFMAN_OPERATOR_IMAGE_PULLSPEC}\"|g" \
       -e "s|quay.io/bpfman/bpfman-operator:latest*|\"${BPFMAN_OPERATOR_IMAGE_PULLSPEC}\"|g" \
	     "${CSV_FILE}"

export AMD64_BUILT=$(skopeo inspect --raw docker://${BPFMAN_OPERATOR_IMAGE_PULLSPEC} | jq -e '.manifests[] | select(.platform.architecture=="amd64")')
export ARM64_BUILT=$(skopeo inspect --raw docker://${BPFMAN_OPERATOR_IMAGE_PULLSPEC} | jq -e '.manifests[] | select(.platform.architecture=="arm64")')
export PPC64LE_BUILT=$(skopeo inspect --raw docker://${BPFMAN_OPERATOR_IMAGE_PULLSPEC} | jq -e '.manifests[] | select(.platform.architecture=="ppc64le")')
export S390X_BUILT=$(skopeo inspect --raw docker://${BPFMAN_OPERATOR_IMAGE_PULLSPEC} | jq -e '.manifests[] | select(.platform.architecture=="s390x")')

export EPOC_TIMESTAMP=$(date +%s)
# time for some direct modifications to the csv
python3 - << CSV_UPDATE
import os
from collections import OrderedDict
from sys import exit as sys_exit
from datetime import datetime
from ruamel.yaml import YAML
yaml = YAML()
def load_manifest(pathn):
   if not pathn.endswith(".yaml"):
      return None
   try:
      with open(pathn, "r") as f:
         return yaml.load(f)
   except FileNotFoundError:
      print("File can not found")
      exit(2)

def dump_manifest(pathn, manifest):
   with open(pathn, "w") as f:
      yaml.dump(manifest, f)
   return
timestamp = int(os.getenv('EPOC_TIMESTAMP'))
datetime_time = datetime.fromtimestamp(timestamp)
bpfman_operator_csv = load_manifest(os.getenv('CSV_FILE'))
# Add arch support labels
bpfman_operator_csv['metadata']['labels'] = bpfman_operator_csv['metadata'].get('labels', {})
if os.getenv('AMD64_BUILT'):
	bpfman_operator_csv['metadata']['labels']['operatorframework.io/arch.amd64'] = 'supported'
if os.getenv('ARM64_BUILT'):
	bpfman_operator_csv['metadata']['labels']['operatorframework.io/arch.arm64'] = 'supported'
if os.getenv('PPC64LE_BUILT'):
	bpfman_operator_csv['metadata']['labels']['operatorframework.io/arch.ppc64le'] = 'supported'
if os.getenv('S390X_BUILT'):
	bpfman_operator_csv['metadata']['labels']['operatorframework.io/arch.s390x'] = 'supported'
bpfman_operator_csv['metadata']['labels']['operatorframework.io/os.linux'] = 'supported'
bpfman_operator_csv['metadata']['annotations']['createdAt'] = datetime_time.strftime('%d %b %Y, %H:%M')
bpfman_operator_csv['metadata']['annotations']['features.operators.openshift.io/disconnected'] = 'true'
bpfman_operator_csv['metadata']['annotations']['features.operators.openshift.io/fips-compliant'] = 'true'
bpfman_operator_csv['metadata']['annotations']['features.operators.openshift.io/proxy-aware'] = 'false'
bpfman_operator_csv['metadata']['annotations']['features.operators.openshift.io/tls-profiles'] = 'false'
bpfman_operator_csv['metadata']['annotations']['features.operators.openshift.io/token-auth-aws'] = 'false'
bpfman_operator_csv['metadata']['annotations']['features.operators.openshift.io/token-auth-azure'] = 'false'
bpfman_operator_csv['metadata']['annotations']['features.operators.openshift.io/token-auth-gcp'] = 'false'
bpfman_operator_csv['metadata']['annotations']['repository'] = 'https://github.com/bpfman/bpfman-operator'
bpfman_operator_csv['metadata']['annotations']['containerImage'] = os.getenv('BPFMAN_OPERATOR_IMAGE_PULLSPEC', '')

dump_manifest(os.getenv('CSV_FILE'), bpfman_operator_csv)
CSV_UPDATE

cat $CSV_FILE
