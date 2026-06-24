#!/usr/bin/env bash

# This script can be called from the base source directory of the repository,
# or from within the "hack/" subdirectory where the script lives. Otherwise
# bail because the output is dumped to a known location and calling from any
# other directories will cause issues.
CALL_POPD=false
if [[ "$PWD" != */hack ]]; then
    pushd hack &>/dev/null || exit
fi

DEBUG=${DEBUG:-false}
OUTPUT_DIR=${OUTPUT_DIR:-""}
CRD_1=${CRD_1:-""}
CRD_2=${CRD_2:-""}
CRD_3=${CRD_3:-""}
CRD_4=${CRD_4:-""}
CRD_5=${CRD_5:-""}
CRD_6=${CRD_6:-""}
CRD_7=${CRD_7:-""}
CRD_8=${CRD_8:-""}

CRD_LIST=(
  ${CRD_1}
  ${CRD_2}
  ${CRD_3}
  ${CRD_4}
  ${CRD_5}
  ${CRD_6}
  ${CRD_7}
  ${CRD_8}
)

process-command() {
  local object=$1
  local filename=$2
  local -a subObject=()

  # DEBUG
  if [[ "$DEBUG" == true ]]; then
    echo "\$ kubectl explain $object"
  fi

  # Run the "kubectl explain" command and dump output to file.
  echo "\$ kubectl explain $object" >> ${filename}
  cmdOutput=$(kubectl explain $object)
  echo "${cmdOutput}" >> ${filename}
  echo "" >> ${filename}

  # Find Sub-Objects, which will be the token before "<Object>".
  # - "<Objects>" may be a list of objects, like "<[]Object>", so
  #   search for *"Object>".
  # - Ignore "<Object>" when the string is in the header, which will
  #   look like "FIELD: status <Object>"
  # Read the ${cmdOutput} line by line and store any Sub-Objects
  # found in the array ${subObject}.
  # Then recursively call this function to repeat the process.
  local substring="Object>"
  local substringIgnore="FIELD:"
  while IFS= read -r line; do
    # Skip lines that don't contain the substring
    [[ "$line" != *"$substring"* ]] && continue

    # Split line into words
    read -ra words <<< "$line"

    # Search through words to find matches of the substring
    for ((i=0; i<${#words[@]}; i++)); do
        if [[ "${words[i]}" == *"$substring" ]]; then
            # Check if two words before is "FIELD:"
            if (( i >= 2 )) && [[ "${words[i-2]}" == "$substringIgnore" ]]; then
                continue  # skip this occurrence
            fi

            # Check if we have a word before to extract
            if (( i >= 1 )); then
                subObject+=("${words[i-1]}")
            fi
        fi
    done
  done <<< "${cmdOutput}"

  for i in "${!subObject[@]}"
  do
    process-command "${object}.${subObject[$i]}" ${filename}
  done
}

# Main

echo ""
echo "WARNING: Script runs \"kubectl explain\" commands against cluster specified in ~/.kube/config."
echo "         Ensure Kubernetes cluster is running latest code."
echo ""

# Test for kubectl or oc
#kubectl version  &>/dev/null
if [ $? != 0 ]; then
  oc version  &>/dev/null
  if [ $? != 0 ]; then
    echo "ERROR: Either \`kubectl\` or \`oc\` must be installed. Exiting ..."
    echo
    exit 1
  fi

  alias kubectl="oc"
fi

# Test for Cluster
kubectl get nodes &>/dev/null
if [ $? != 0 ]; then
  echo "ERROR: Kubernetes Cluster not detected. Exiting ..."
  echo
  exit 1
fi

# Loop through each Custom Resource Definition defined in the list
for i in "${!CRD_LIST[@]}"
do
  if [[ ${CRD_LIST[$i]} != "" ]]; then
    filename="${OUTPUT_DIR}/${CRD_LIST[$i]}.txt"

    rm -f ${filename} || true
    process-command ${CRD_LIST[$i]} ${filename}

    # DEBUG
    if [[ "$DEBUG" == true ]]; then
      echo ""
    fi
  fi
done

if [[ "$CALL_POPD" == true ]]; then
    popd &>/dev/null || exit
fi
