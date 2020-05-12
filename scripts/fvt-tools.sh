#!/usr/bin/env bash

# Wait loop configuration
SLEEP_COUNT=10
SLEEP_WAIT_SECONDS=6
declare -a NODES

fvtlog() {
  echo "$(date +"%F %T %Z")" "[fvt]" "$*"
}

list_nodes() {
  kubectl get nodes --no-headers -o jsonpath='{.items[*].metadata.name}'
}

list_pods() {
  kubectl get pods -A --no-headers --selector=name=staticroute-operator -o wide | awk '{print $2}'
}

get_sr_pod_ns() {
  kubectl get pods -A --no-headers --selector=name=staticroute-operator -o wide | awk 'NR==1{print $1}'
}

create_hostnet_pods() {
  for node in $(list_nodes); do
    kubectl run --generator=run-pod/v1 hostnet-${node} --labels="fvt-helper=hostnet" --overrides='{"apiVersion": "v1", "spec": {"nodeSelector": { "kubernetes.io/hostname": "${node}" }}}' --overrides='{"kind":"Pod", "apiVersion":"v1", "spec": {"hostNetwork":true}}' --image busybox -- /bin/tail -f /dev/null
  done
}

delete_hostnet_pods() {
  kubectl delete po --selector fvt-helper=hostnet
}

# Function to execute a command on the host network of a node, selected by a pod that is running on it
# Parameters:
# - Namespace
# - Pod name
# - Command
exec_in_hostnet_of_pod() {
  kubectl exec -ti hostnet-$(kubectl get po -n $1 $2 -o jsonpath='{.spec.nodeName}') $3
}

get_default_gw() {
  local sr_ns 
  sr_ns=$(get_sr_pod_ns)
  [[ "${PROVIDER}" == "ibmcloud" ]] && v="10.0.0.0/8" || v="default"
  exec_in_hostnet_of_pod "${sr_ns}" "${PODS[0]}" 'ip route' | grep "^${v}.*via.*dev" | awk '{print $3}'
}

# Function to check the CR status
# Parameters:
# - CR name
# - Node name (optional, valid values: all_nodes_shall_post_status/nodes_shall_not_post_status/specific node)
# - Error string to check (default is empty)
check_staticroute_crd_status() {
  set +e
  local cr=$1
  local match_node="${2:-all_nodes_shall_post_status}"
  local error_string="${3:-}"
  local status_ok=false
  for _ in $(seq ${SLEEP_COUNT}); do
    if [[ "${match_node}" == "all_nodes_shall_post_status" ]]; then
      cr_array=($(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*].hostname}'))
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
  local error_array=($(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*].error}'))
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

# Function to check the staticroute-operator pods are all running
check_operator_is_running() {
  set +e
  local reached_expected_count=false
  for _ in $(seq ${SLEEP_COUNT}); do
    number_of_pods_not_running=$(kubectl get pods -A --selector name=staticroute-operator --no-headers | grep -vc Running)
    if [[ $number_of_pods_not_running -eq 0 ]]; then
      reached_expected_count=true
      break
    else
      sleep ${SLEEP_WAIT_SECONDS}
    fi
  done
  set -e
  if [[ $reached_expected_count == false ]]; then
    fvtlog "Failed to get running status for the staticroute-operator pods. Could it pull its image?"
    return 2
  fi
}

# Function to check the route table in a container (pods are hostnetwork)
# Parameters:
# - CR name
# - Node name (optional, needed when a CR applies only for a given node)
# - Test type which is able to differentiate positive or negative tests
check_route_in_container() {
  local route=$1
  local match_node="${2:-all}"
  local test_type="${3:-positive}"
  local match=false
  local sr_ns 
  sr_ns=$(get_sr_pod_ns)
  for node in "${PODS[@]}"; do
    # Execute the command on all the nodes or only the given node
    if [[ "${match_node}" == "all" ]] || 
       [[ "${match_node}" == "${node}" ]]; then
      match=true
      routes=$(exec_in_hostnet_of_pod "${sr_ns}" "${node}" 'ip route')
      echo '**** ' $routes
      if [[ "${test_type}" == "positive" ]] &&
         [[ ${routes} == *${route}* ]]; then
        fvtlog "Passed: The route was found on node ${node}!"
      elif [[ "${test_type}" == "negative" ]] &&
           [[ ${routes} != *${route}* ]]; then
        fvtlog "Passed: As expected, the route was not found on node ${node}!"
      else
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

apply_common_operator_resources() {
  fvtlog "Apply common staticoperator related resources..."
  declare -a common_resources=('crds/static-route.ibm.com_staticroutes_crd.yaml' 'service_account.yaml' 'role.yaml' 'role_binding.yaml');
  for resource in "${common_resources[@]}"; do
    kubectl apply -f "${SCRIPT_PATH}"/../deploy/"${resource}"
  done

  fvtlog "Install the staticroute-operator..."
  cp "${SCRIPT_PATH}"/../deploy/operator.yaml "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
  sed -i "s|REPLACE_IMAGE|${REGISTRY_REPO}:${CONTAINER_VERSION}|g" "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
  sed -i "s|Always|IfNotPresent|g" "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
  if [[ ${IMAGEPULLSECRET} ]]; then
    sed -i "s|hostNetwork: true|&\n      imagePullSecrets:\n      - name: ${IMAGEPULLSECRET}|" deploy/operator.dev.yaml
  fi
  kubectl apply -f "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
}

# Get a node that is running the given pod in $1
get_node_by_pod() {
  local sr_ns
  sr_ns=$(get_sr_pod_ns)
  kubectl get po "$1" -n"${sr_ns}" --no-headers -owide | awk '{print $7}'
}

# Return the first item from the given list
pick_protected_subnet() {
  echo "${1}" | cut -d, -f1
}
