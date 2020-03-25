#!/usr/bin/env bash

# Wait loop configuration
SLEEP_COUNT=30
SLEEP_WAIT_SECONDS=2
declare -a NODES

fvtlog() {
  echo "$(date +"%F %T %Z")" "[fvt]" "$*"
}

list_nodes() {
  NODES=($(kubectl get nodes --selector "node-role.kubernetes.io/master notin ()" --no-headers -o jsonpath='{.items[*].metadata.name}'))
}

# Function to check the CR status
# Parameters:
# - CR name
# - Node name (optional, needed when a CR applies only for a given node)
check_staticroute_crd_status() {
  set +e
  cr=$1
  match_node="${2:-all}"
  status_ok=false
  for _ in $(seq ${SLEEP_COUNT}); do
    if [[ "${match_node}" == "all" ]]; then
      cr_array=($(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*].hostname}'))
      if [[ ${#NODES[*]} -eq ${#cr_array[*]} ]]; then
          status_ok=true
          break
      fi
    else
      node=$(kubectl get staticroute "${cr}" --no-headers -o jsonpath='{.status.nodeStatus[*].hostname}' | grep "${match_node}")
      if [[ "${node}" == "${node}" ]]; then
          status_ok=true
          break
      fi
    fi
    sleep ${SLEEP_WAIT_SECONDS}
  done
  if [[ $status_ok == "false" ]]; then
    fvtlog "Failed to get the nodeStatus for the ${cr}. Are the operator pods running?"
    return 1
  fi
  fvtlog "Passed: ${cr} status is updated and contains the necessary hosts."
  set -e
}

# Function to check the staticroute-operator pods are all running
check_operator_is_running() {
  set +e
  reached_expected_count=false
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
check_route_in_container() {
  route=$1
  match_node="${2:-all}"
  test_type="${3:-positive}"
  for node in "${NODES[@]}"; do
    # Execute the command on all the nodes or only the given node
    if [[ "${match_node}" == "all" ]] || 
       [[ "${match_node}" == "${node}" ]]; then
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
    else
      continue
    fi
  done
}
