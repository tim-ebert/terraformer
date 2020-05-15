#!/bin/bash -eu
#
# Copyright (c) 2017 SAP SE or an SAP affiliate company. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

DIR_STATE_IN="/tfstate"
DIR_STATE_OUT="/tfstate-out"
DIR_CONFIGURATION="/tf"
DIR_VARIABLES="/tfvars"
DIR_PROVIDERS="/terraform-providers"
DIR_PLUGIN_BINARIES=".terraform/plugins/linux_amd64"

PATH_STATE_CONFIG_MAP="$DIR_STATE_OUT/$TF_STATE_CONFIG_MAP_NAME"
PATH_STATE_IN="$DIR_STATE_IN/terraform.tfstate"
PATH_STATE_OUT="$DIR_STATE_OUT/terraform.tfstate"
PATH_CONFIGURATION_MAINTF="$DIR_CONFIGURATION/main.tf"
PATH_CONFIGURATION_VARIABLESTF="$DIR_CONFIGURATION/variables.tf"
PATH_VARIABLES="$DIR_VARIABLES/terraform.tfvars"

NAMESPACE="$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)"

# exponential backoff functionality
: ${MAX_TIME_SEC:=1800}
: ${MAX_BACKOFF_SEC:=30}

function backoff() {
  t=1
  start="$(date +%s)"

  while ! "${@}"; do
    now="$(date +%s)"
    if [[ $((start + MAX_TIME_SEC)) -le $now ]]; then
      echo "$(date) Timeout of ${MAX_TIME_SEC}s reached. Aborting"
      return 1
    fi

    echo "$(date) Repeating command in $t sec..."
    sleep $t

    t=$((t * 2))
    if [[ "$t" -ge "${MAX_BACKOFF_SEC}" ]]; then
      t=${MAX_BACKOFF_SEC}
    fi
  done

  echo "$(date) Command executed successful."
}

# determine command
command="${1:-apply}"

# default exit code
exitcode=1
received_termination_signal=0

mkdir -p "$DIR_CONFIGURATION"
mkdir -p "$DIR_VARIABLES"
mkdir -p "$DIR_STATE_IN"
mkdir -p "$DIR_STATE_OUT"

# live lookup of terraform resources stored in configmaps
echo "Fetching configmap $NAMESPACE/$TF_CONFIGURATION_CONFIG_MAP_NAME and storing data in $PATH_CONFIGURATION_MAINTF..."
kubectl \
  --namespace="$NAMESPACE" \
  get configmap "$TF_CONFIGURATION_CONFIG_MAP_NAME" \
  --output="jsonpath={.data['main\.tf']}" \
  > "$PATH_CONFIGURATION_MAINTF"

echo "Fetching configmap $NAMESPACE/$TF_CONFIGURATION_CONFIG_MAP_NAME and storing data in $PATH_CONFIGURATION_VARIABLESTF..."
kubectl \
  --namespace="$NAMESPACE" \
  get configmap "$TF_CONFIGURATION_CONFIG_MAP_NAME" \
  --output="jsonpath={.data['variables\.tf']}" \
  > "$PATH_CONFIGURATION_VARIABLESTF"

echo "Fetching secret $NAMESPACE/$TF_VARIABLES_SECRET_NAME and storing data in $PATH_VARIABLES..."
kubectl \
  --namespace="$NAMESPACE" \
  get secret "$TF_VARIABLES_SECRET_NAME" \
  --output="jsonpath={.data['terraform\.tfvars']}" | \
  base64 -d > "$PATH_VARIABLES"

echo "Fetching configmap $NAMESPACE/$TF_STATE_CONFIG_MAP_NAME and storing data in $PATH_STATE_IN..."
kubectl \
  --namespace="$NAMESPACE" \
  get configmap "$TF_STATE_CONFIG_MAP_NAME" \
  --output="jsonpath={.data['terraform\.tfstate']}" \
  > "$PATH_STATE_IN"

# required to initialize the provider plugins
terraform init -plugin-dir="$DIR_PROVIDERS" "$DIR_CONFIGURATION"
# workaround for `terraform init`; required to make `terraform validate` work (plugin_path file ignored?)
cp -r "$DIR_PROVIDERS"/* "$DIR_PLUGIN_BINARIES"/.

function wait_for_tf() {
  wait $1
  exitcode=$?
  if [[ $exitcode -eq 0 ]]; then
    echo "$(date) Terraform process $1 completed."
    exitcode=$received_termination_signal
  else
    echo "$(date) Terraform process $1 exited with a non-zero code $exitcode."
  fi
}

# graceful shutdown function storing state in the configmap
function end_execution() {
  # Delete trap handler to avoid recursion
  trap - INT TERM EXIT

  if [ -n "$TF_PID" ] && kill -0 "$TF_PID" &>/dev/null; then
    echo "$(date) Sending SIGTERM to terraform process $TF_PID."
    kill -SIGTERM $TF_PID
    echo "$(date) Waiting for terraform process $TF_PID to complete..."
  fi
  wait_for_tf $TF_PID

  # check whether the terraform state has changed
  if [[ ! -f "$PATH_STATE_OUT" ]] || diff "$PATH_STATE_IN" "$PATH_STATE_OUT" 1> /dev/null; then # passes (returns 0) if there is no diff
    echo -e "\nNothing to do."
    exit $exitcode
  fi

  # update config map with the new terraform state
  echo -e "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: $TF_STATE_CONFIG_MAP_NAME\ndata:\n  terraform.tfstate: |" > "$PATH_STATE_CONFIG_MAP"
  cat "$PATH_STATE_OUT" | sed -n 's/^/    /gp' >> "$PATH_STATE_CONFIG_MAP"

  function update_state_configmap() {
    kubectl \
      replace \
      -f "$PATH_STATE_CONFIG_MAP" \
      > /dev/null

    # validate that the current terraform state is properly reflected in the config map
    kubectl \
      --namespace="$NAMESPACE" \
      get configmap "$TF_STATE_CONFIG_MAP_NAME" \
      --output="jsonpath={.data['terraform\.tfstate']}" \
      > "$PATH_STATE_CONFIG_MAP.put"

    if diff "$PATH_STATE_OUT" "$PATH_STATE_CONFIG_MAP.put" 1> /dev/null; then # passes (returns 0) if there is no diff
      echo -e "\nConfigMap successfully updated with terraform state."
      return 0
    else
      return 1
    fi
  }

  if ! backoff update_state_configmap; then
    # dump terraform state so that we can find it at least in the logs
    echo -e "\nConfigMap could not be updated with terraform state! Dumping state file now to have it in the logs:"
    cat "$PATH_STATE_OUT"
    exit 1
  fi

  exit $exitcode
}

if [[ "$command" == "validate" ]]; then
  terraform \
    validate \
    -var-file="$PATH_VARIABLES" \
    "$DIR_CONFIGURATION"
  if [[ "$?" == "0" ]]; then
    terraform \
      plan \
      -parallelism=4 \
      -detailed-exitcode \
      -state="$PATH_STATE_IN" \
      -var-file="$PATH_VARIABLES" \
      "$DIR_CONFIGURATION"
  else
    exit $?
  fi
else
  # Install trap handler for proper cleanup, summary and result code
  trap "exit 100" HUP QUIT PIPE
  trap "received_termination_signal=143" INT TERM
  trap end_execution INT TERM EXIT

  TF_PID=

  if [[ "$command" == "apply" ]]; then
    terraform \
      apply \
      -auto-approve \
      -parallelism=4 \
      -state="$PATH_STATE_IN" \
      -state-out="$PATH_STATE_OUT" \
      -var-file="$PATH_VARIABLES" \
      "$DIR_CONFIGURATION" &
    TF_PID=$!
    wait_for_tf $TF_PID
    exit $exitcode
  elif [[ "$command" == "destroy" ]]; then
    terraform \
      destroy \
      -auto-approve \
      -parallelism=4 \
      -state="$PATH_STATE_IN" \
      -state-out="$PATH_STATE_OUT" \
      -var-file="$PATH_VARIABLES" \
      "$DIR_CONFIGURATION" &
    TF_PID=$!
    echo "$(date) Waiting for terraform process $TF_PID to complete..."
    wait_for_tf $TF_PID
    exit $exitcode
  else
    # Delete trap handler - nothing to do
    trap - HUP INT QUIT PIPE TERM EXIT
    echo -e "\nUnknown command: $command"
    exit 1
  fi
fi
