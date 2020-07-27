#!/usr/bin/env bash

# Wait loop configuration
SLEEP_COUNT=10
SLEEP_WAIT_SECONDS=6
declare -a NODES

fvtlog() {
  echo "$(date +"%F %T %Z")" "[fvt]" "$*"
}

update_node_list() {
  mapfile -d' ' -t NODES < <(kubectl get nodes --no-headers -o jsonpath='{.items[*].metadata.name}')
}

pick_non_master_node() {
  if [[ "${PROVIDER}" == "ibmcloud" ]]; then
    echo -ne "${NODES[0]}"
  else
    for index in ${!NODES[*]}
    do
      kubectl get no "${NODES[$index]}" --show-labels | grep 'node-role.kubernetes.io/master=' > /dev/null && continue || echo -ne "${NODES[$index]}"; break
    done
  fi
}

create_hostnet_pods() {
  for index in ${!NODES[*]}
  do
    kubectl run --generator=run-pod/v1 hostnet-"${NODES[$index]//\./-}" --labels="fvt-helper=hostnet" --overrides="{\"apiVersion\": \"v1\", \"spec\": {\"hostNetwork\":true, \"nodeSelector\": { \"kubernetes.io/hostname\": \"${NODES[$index]}\" }, \"tolerations\": [{ \"operator\": \"Exists\" }]}}" --image busybox -- /bin/tail -f /dev/null
  done
  local status_ok=false
  for _ in $(seq ${SLEEP_COUNT}); do
    actual=$(kubectl get pods --selector=fvt-helper=hostnet --field-selector=status.phase=Running --no-headers | wc -l)
    expected=$(kubectl get pods --selector=fvt-helper=hostnet --no-headers | wc -l)
    fvtlog "Waiting for hostnet helper pods to come up. Actual: ${actual}, expected: ${expected}"
    if [[ "${actual}" -eq "${expected}" ]]; then
      status_ok=true
      break
    fi
    sleep ${SLEEP_WAIT_SECONDS}
  done
  if [[ ${status_ok} == false ]]; then
    fvtlog "Failed to start hostnet helper pod."
    return 4
  fi
}

delete_hostnet_pods() {
  kubectl delete po --selector fvt-helper=hostnet
}

# Function to execute a command on the host network of a node, selected by a pod that is running on it
# Parameters:
# - Node name
# - Command (may be multiple string)
exec_in_hostnet_of_node() {
  local nodename=$1
  shift
  kubectl exec hostnet-"${nodename//\./-}" -- sh -c "$@"
}

get_default_gw() {
  local nodename=$1
  if [[ "${PROVIDER}" == "ibmcloud" ]]; then
    echo "127.0.0.1"
  else
    exec_in_hostnet_of_node "${NODES[0]}" 'ip route' | grep "^default.*via.*dev" | awk '{print $3}'
  fi
  exec_in_hostnet_of_node "${nodename}" 'ip route' | grep "^${v}.*via.*dev" | awk '{print $3}'
}

get_provider_type() {
    local provider_type
    provider_type=$(kubectl get nodes --no-headers --selector ibm-cloud.kubernetes.io/iaas-provider=softlayer | wc -l)
    if [[ ${provider_type} != "0" ]]; then
        echo "softlayer"
        return
    fi
    provider_type=$(kubectl get nodes --no-headers --selector ibm-cloud.kubernetes.io/iaas-provider=gc | wc -l)
    if [[ ${provider_type} != "0" ]]; then
        echo "gen1"
        return
    fi
    provider_type=$(kubectl get nodes --no-headers --selector ibm-cloud.kubernetes.io/iaas-provider=g2 | wc -l)
    if [[ ${provider_type} != "0" ]]; then
        echo "gen2"
        return
    fi
    echo "Error: provider not found"
}

# Function to check the CR status
# Parameters:
# - CR name
# - Node name (optional, valid values: all_nodes_shall_post_status/nodes_shall_not_post_status/specific node)
# - Error string to check (default is empty)
check_static_route_crd_status() {
  set +e
  local cr=$1
  local match_node="${2:-all_nodes_shall_post_status}"
  local error_string="${3:-}"
  local status_ok=false
  for _ in $(seq ${SLEEP_COUNT}); do
    if [[ "${match_node}" == "all_nodes_shall_post_status" ]]; then
      mapfile -d' ' -t cr_array < <(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*].hostname}')
      if [[ ${#NODES[*]} -eq ${#cr_array[*]} ]]; then
        status_ok=true
        break
      fi
    elif [[ "${match_node}" == "nodes_shall_not_post_status" ]]; then
      node_status=$(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*]}')
      cr_exists=$?
      if [[ "${cr_exists}" == 0 ]] &&
        [[ "${node_status}" == "" ]]; then
        status_ok=true
        break
      fi
    else
      node_exists=$(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*].hostname}' | grep -c "${match_node}")
      if [[ "${node_exists}" == 1 ]]; then
        status_ok=true
        break
      fi
    fi
    sleep ${SLEEP_WAIT_SECONDS}
  done

  # Get all the error fields and word by word put to an array
  mapfile -d* -t error_array < <(kubectl get staticroute "${cr}" --no-headers -o go-template --template="{{range .status.nodeStatus}}{{if .error}}{{.error}}*{{end}}{{end}}")
  if [[ "${error_string}" != "" ]]; then
    if [[ "${#error_array[*]}" == 0 ]]; then
      status_ok=false
    else
      for error in "${error_array[@]}"; do
        if [[ "${error_string}" != *${error}* ]]; then
          status_ok=false
          break
        fi
      done
    fi
  elif [[ "${#error_array[*]}" != 0 ]]; then
    fvtlog "Unexpected errors found: " "${error_array[@]}"
    status_ok=false
  fi
  set -e

  if [[ ${status_ok} == false ]]; then
    fvtlog "Failed to get the nodeStatus for the ${cr}. Are the operator pods running?"
    return 1
  fi
  fvtlog "Passed: ${cr} status is updated and contains the expected values."
}

# Function to check the static-route-operator pods are all running
check_operator_is_running() {
  set +e
  local reached_expected_count=false
  for _ in $(seq ${SLEEP_COUNT}); do
    number_of_pods_not_running=$(kubectl get pods -A --selector name=static-route-operator --no-headers | grep -vc Running)
    if [[ $number_of_pods_not_running -eq 0 ]]; then
      reached_expected_count=true
      break
    else
      sleep ${SLEEP_WAIT_SECONDS}
    fi
  done
  set -e
  if [[ $reached_expected_count == false ]]; then
    fvtlog "Failed to get running status for the static-route-operator pods. Could it pull its image?"
    return 2
  fi
}

# Function to check the route table on nodes
# Parameters:
# - CR name
# - Node name (optional, needed when a CR applies only for a given node)
# - Test type which is able to differentiate positive or negative tests
check_route_on_nodes() {
  local route=$1
  local match_node="${2:-all}"
  local test_type="${3:-positive}"
  local match=false
  local passed=false
  local routes
  for node in "${NODES[@]}"; do
    # Execute the command on all the nodes or only the given node
    if [[ "${match_node}" == "all" ]] || 
       [[ "${match_node}" == "${node}" ]]; then
      match=true
      passed=false
      for _ in $(seq ${SLEEP_COUNT}); do
        routes=$(exec_in_hostnet_of_node "${node}" 'ip route')
        if [[ "${test_type}" == "positive" ]] &&
          [[ ${routes} == *${route}* ]]; then
          fvtlog "Passed: The route was found on node ${node}!"
          passed=true
          break
        elif [[ "${test_type}" == "negative" ]] &&
            [[ ${routes} != *${route}* ]]; then
          fvtlog "Passed: As expected, the route was not found on node ${node}!"
          passed=true
          break
        else
          sleep ${SLEEP_WAIT_SECONDS}
        fi
      done

      if [[ "${passed}" == false ]]; then
        fvtlog "Failure in check route on node ${node} - \"${route}\" (${test_type})"
        fvtlog "Routes on the node: ${routes}"
        return 3
      fi
    fi
  done
  if [[ "${match}" == false ]]; then
    fvtlog "Failure in check route on node: there were no matching node for the parameter ${match_node}!"
    return 1
  fi
}

label_nodes_with_default() {
    local zone=$1
    for node in "${NODES[@]}"; do
      kubectl label node "${node}" failure-domain.beta.kubernetes.io/zone="${zone}" --overwrite
      kubectl label node "${node}" kubernetes.io/hostname="${node}" --overwrite=true
    done
}

create_kind_cluster() {
  kind --version || (echo "Please install kind before running fvt tests"; exit 1)

  fvtlog "Creating Kubernetes cluster with kind"
  if [[ "$(kind get clusters -q | grep -c "${KIND_CLUSTER_NAME}")" != 1 ]]; then
    cat <<EOF | kind create cluster --name "${KIND_CLUSTER_NAME}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
EOF
  else
    fvtlog "Warning! Running on existing cluster!"
  fi
}

manage_common_operator_resources() {
  local action=$1
  fvtlog "${action^} common static-route-operator related resources..."
  declare -a common_resources=('crds/static-route.ibm.com_staticroutes_crd.yaml' 'service_account.yaml' 'role.yaml' 'role_binding.yaml');
  for resource in "${common_resources[@]}"; do
    kubectl "${action}" -f "${SCRIPT_PATH}"/../deploy/"${resource}"
  done

  fvtlog "${action^} the static-route-operator..."
  cp "${SCRIPT_PATH}"/../deploy/operator.yaml "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
  sed -i "s|REPLACE_IMAGE|${REGISTRY_REPO}:${CONTAINER_VERSION}|g" "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
  sed -i "s|Always|IfNotPresent|g" "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
  if [[ ${IMAGEPULLSECRET} ]]; then
    sed -i "s|hostNetwork: true|&\n      imagePullSecrets:\n      - name: ${IMAGEPULLSECRET}|" "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
  fi
  kubectl "${action}" -f "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
}

# Return the first item from the given list
pick_protected_subnet() {
  echo "${1}" | cut -d, -f1
}
