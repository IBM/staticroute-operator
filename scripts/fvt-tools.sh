#!/usr/bin/env bash

# Wait loop configuration
SLEEP_COUNT=10
SLEEP_WAIT_SECONDS=6
declare -a NODES

fvtlog() {
  echo "$(date +"%F %T %Z")" "[fvt]" "$*"
}

list_nodes() {
  kubectl get nodes --selector "node-role.kubernetes.io/master notin ()" --no-headers -o jsonpath='{.items[*].metadata.name}'
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
    else
      node_exists=$(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*].hostname}' | grep -c "${match_node}")
      if [[ "${node_exists}" == 1 ]]; then
        status_ok=true
        break
      fi
    fi
    sleep ${SLEEP_WAIT_SECONDS}
  done

  # So far, the script was waiting the wait loop, do one more check for the resource
  if [[ "${match_node}" == "nodes_shall_not_post_status" ]]; then
    node_status=$(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*]}')
    cr_exists=$?
    if [[ "${cr_exists}" == 0 ]] &&
       [[ "${node_status}" == "" ]]; then
      status_ok=true
    fi
  fi
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
    number_of_pods_not_running=$(kubectl get pods --selector name=staticroute-operator --no-headers | grep -vc Running)
    if [[ $number_of_pods_not_running -eq 0 ]]; then
      reached_expected_count=true
      break
    else
      sleep ${SLEEP_WAIT_SECONDS}
    fi
  done
  if [[ $reached_expected_count == false ]]; then
    fvtlog "Failed to get running status for the staticroute-operator pods. Could it pull its image?"
    return 2
  fi
  set -e
}

# Function to check the route table in a container
# Parameters:
# - CR name
# - Node name (optional, needed when a CR applies only for a given node)
# - Test type which is able to differentiate positive or negative tests
check_route_in_container() {
  local route=$1
  local match_node="${2:-all}"
  local test_type="${3:-positive}"
  local match=false
  for node in "${NODES[@]}"; do
    # Execute the command on all the nodes or only the given node
    if [[ "${match_node}" == "all" ]] || 
       [[ "${match_node}" == "${node}" ]]; then
      match=true
      routes=$(docker exec "${node}" ip route)
      if [[ "${test_type}" == "positive" ]] &&
         [[ ${routes} == *${route}* ]]; then
        fvtlog "Passed: It's there on node ${node}!"
      elif [[ "${test_type}" == "negative" ]] &&
           [[ ${routes} != *${route}* ]]; then
        fvtlog "Passed: It's expected to not be the there on ${node}!"
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
