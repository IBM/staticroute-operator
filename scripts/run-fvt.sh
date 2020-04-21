#!/usr/bin/env bash
set -e
set -o pipefail

SCRIPT_PATH=$PWD/$(dirname "$0")
KIND_CLUSTER_NAME="staticroute-operator-fvt"
DEBUG="${DEBUG:-false}"
SKIP_OPERATOR_INSTALL="${SKIP_OPERATOR_INSTALL:-false}"
# shellcheck source=scripts/fvt-tools.sh
. "${SCRIPT_PATH}/fvt-tools.sh"

cleanup() {
  fvtlog "Running cleanup, error code $?"
  if [[ ! "${PROVIDER}" ]]
  then
    if [[ "${DEBUG}" == "false" ]]; then
      kind delete cluster --name ${KIND_CLUSTER_NAME}
      rm -rf "${SCRIPT_PATH}"/kubeconfig.yaml
    fi
  else
    [[ "${DELETED_NODE_MANIFEST}" ]] && echo "${DELETED_NODE_MANIFEST}" | kubectl apply -f -
  fi
}

trap cleanup EXIT

fvtlog "Preparing environment for staticroute-operator tests..."

## prepare environment for testing
case "${PROVIDER}" in
  ibmcloud)
    fvtlog "ibmcloud set as provider, using an existing cluster"

    # for manual install
    if [[ "${SKIP_OPERATOR_INSTALL}" == false ]]
    then
        apply_common_operator_resources
    else
        kubectl delete staticroute --all &>/dev/null
    fi
    ;;
  *)
    fvtlog "No provider set, using KinD as the default."
    create_kind_cluster
    
    # Get KUBECONFIG
    kind get kubeconfig --name "${KIND_CLUSTER_NAME}" > "${SCRIPT_PATH}"/kubeconfig.yaml

    fvtlog "Loading the staticrouter operator image to the cluster..."
    kind load docker-image --name="${KIND_CLUSTER_NAME}" "${REGISTRY_REPO}":"${CONTAINER_VERSION}"

    apply_common_operator_resources
    ;;
esac
# Restore default labels on nodes
label_nodes_with_default "zone01"

## start the actual tests
fvtlog "Starting staticroute-operator fvt testing..."
fvtlog "Check if the staticroute-operator pods are running..."
check_operator_is_running
fvtlog "OK"

# Get all the worker nodes
NODES=($(list_nodes))

# Get all operator pods
PODS=($(list_pods))

# Choose a node to test selector case
A_NODE=($(get_node_by_pod ${PODS[1]}))

# Get default gateway
GW=$(get_default_gw)
fvtlog "Choosing Gateway: ${GW}"
fvtlog "Choosing K8s node as selector tests: ${A_NODE}"

fvtlog "Start applying staticroute configurations"
cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-simple
spec:
  subnet: "192.168.0.0/24"
---
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-with-gateway
spec:
  subnet: "192.168.1.0/24"
  gateway: "${GW}"
---
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-with-selector
spec:
  subnet: "192.168.2.0/24"
  selectors:
    -
      key: "kubernetes.io/hostname"
      operator: In
      values:
        - ${A_NODE}
EOF

fvtlog "Check if the staticroute CRs have valid nodeStatus..."
check_staticroute_crd_status "example-staticroute-simple"
check_staticroute_crd_status "example-staticroute-with-gateway"
check_staticroute_crd_status "example-staticroute-with-selector" "${A_NODE}"

fvtlog "Test example-staticroute-simple - Check that all workers have applied the route to 192.168.0.0/24"
check_route_in_container "192.168.0.0/24 via ${GW}"

fvtlog "Test example-staticroute-with-gateway - Check that all workers have applied the route to 192.168.1.0/24 with the defined next-hop address"
check_route_in_container "192.168.1.0/24 via ${GW}"

fvtlog "Test example-staticroute-with-selector - Check that only worker2 has applied the route to 192.168.2.0/24"
check_route_in_container "192.168.2.0/24 via ${GW}" "${PODS[0]}" "negative"
check_route_in_container "192.168.2.0/24 via ${GW}" "${PODS[1]}"

fvtlog "Test selected label is changed - "${PODS[1]}" has to remove its route"
kubectl label node "${A_NODE}" kubernetes.io/hostname=temp --overwrite=true
check_staticroute_crd_status "example-staticroute-with-selector" "nodes_shall_not_post_status"
check_route_in_container "192.168.2.0/24 via ${GW}" "${PODS[1]}" "negative"

fvtlog "And then apply back the label - "${PODS[1]}" has to restore its route"
kubectl label node "${A_NODE}" kubernetes.io/hostname="${A_NODE}" --overwrite=true
check_staticroute_crd_status "example-staticroute-with-selector" "${A_NODE}"
check_route_in_container "192.168.2.0/24 via ${GW}" "${PODS[1]}"

# since testing node remove/re-add is bit slow and complicated on a real cluster
# this just goes with KinD (default) provider
if [[ ! ${PROVIDER} ]]
then
  fvtlog "Test node failure. Other node shall cleanup the status on behalf of the failed node"
  DELETED_NODE_NAME="${A_NODE}"
  DELETED_NODE_MANIFEST="$(kubectl get no "${A_NODE}" -o yaml)"

  docker pause "${A_NODE}"
  kubectl delete no "${A_NODE}"

  NODES=($(list_nodes))
  check_staticroute_crd_status "example-staticroute-simple"
  check_staticroute_crd_status "example-staticroute-with-gateway"
  check_staticroute_crd_status "example-staticroute-with-selector" "nodes_shall_not_post_status"
  fvtlog "Restore failed node. It shall catch up again with the routes"
  echo "${DELETED_NODE_MANIFEST}" | kubectl apply -f -
  docker unpause "${DELETED_NODE_NAME}"

  NODES=($(list_nodes))
  check_staticroute_crd_status "example-staticroute-simple"
  check_staticroute_crd_status "example-staticroute-with-gateway"
  check_staticroute_crd_status "example-staticroute-with-selector" "${A_NODE}"
else
  fvtlog "Provider is not KinD, skipping node failure tests"
fi

PODS=($(list_pods))
fvtlog "Test staticroute deletion - routes must be deleted"
kubectl delete staticroute example-staticroute-simple
check_route_in_container "192.168.0.0/24 via ${GW}" "all" "negative"
kubectl delete staticroute example-staticroute-with-gateway
check_route_in_container "192.168.1.0/24 via ${GW}" "all" "negative"
kubectl delete staticroute example-staticroute-with-selector
check_route_in_container "192.168.2.0/24 via ${GW}" "${PODS[1]}" "negative"

fvtlog "Test wrong gateway configuration"
cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-with-wrong-gateway
spec:
  subnet: "192.168.0.0/24"
  gateway: "172.18.0.1"
EOF
check_staticroute_crd_status "example-staticroute-with-wrong-gateway" "all_nodes_shall_post_status" "Given gateway IP is not directly routable, cannot setup the route"
check_route_in_container "192.168.0.0/24 via 172.18.0.1" "all" "negative"
kubectl delete staticroute example-staticroute-with-wrong-gateway

if [[ ! ${PROVIDER} ]]
then
  fvtlog "Test subnet protection (KinD)"
  sed -i "s|env:|env:\n        - name: PROTECTED_SUBNET_TEST1\n          value: 192.168.1.0\/24,192.168.2.0\/24|" deploy/operator.dev.yaml
  sed -i "s|env:|env:\n        - name: PROTECTED_SUBNET_TEST2\n          value: 192.168.3.0\/24,192.168.4.0\/24,192.168.5.0\/24|" deploy/operator.dev.yaml
  SUBNET1="192.168.1.0/24"
  SUBNET2="192.168.4.0/24"
  kubectl apply -f "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
else
  fvtlog "Test subnet protection"
  if [[ "${PROTECTED_SUBNET_TEST1}" && "${PROTECTED_SUBNET_TEST2}" ]]
  then
    kubectl get ds staticroute-operator -n$(get_sr_pod_ns) -oyaml | \
      sed "s|env:|env:\n        - name: PROTECTED_SUBNET_TEST1\n          value: ${PROTECTED_SUBNET_TEST1}\n        - name: PROTECTED_SUBNET_TEST2\n          value: ${PROTECTED_SUBNET_TEST2}|"\
      | kubectl apply -f -

    SUBNET1=$(pick_protected_subnet "${PROTECTED_SUBNET_TEST1}")
    SUBNET2=$(pick_protected_subnet "${PROTECTED_SUBNET_TEST2}")
  else
    fvtlog "No subnet env variables, skipping subnet protection test"
    SKIP_PROTECTED_SUBNET_TESTS=true
  fi
fi

check_operator_is_running

if [[ ! "${SKIP_PROTECTED_SUBNET_TESTS}" ]]
then
  cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-protected-subnet1
spec:
  subnet: "${SUBNET1}"
---
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-protected-subnet2
spec:
  subnet: "${SUBNET2}"
EOF
  NODES=($(list_nodes))
  PODS=($(list_pods))
  check_staticroute_crd_status "example-staticroute-protected-subnet1" "all_nodes_shall_post_status" "Given subnet overlaps with some protected subnet"
  check_staticroute_crd_status "example-staticroute-protected-subnet2" "all_nodes_shall_post_status" "Given subnet overlaps with some protected subnet"
  check_route_in_container "${SUBNET1} via ${GW}" "all" "negative"
  check_route_in_container "${SUBNET2} via ${GW}" "all" "negative"
  kubectl delete staticroute example-staticroute-protected-subnet1 example-staticroute-protected-subnet2
fi

fvtlog "Test staticroute when selector does not apply - nodes do not have the label, status shall be empty"
cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-no-match
spec:
  subnet: "192.168.0.0/24"
  gateway: "${GW}"
  selectors:
    -
      key: "failure-domain.beta.kubernetes.io/zone"
      operator: In
      values:
        - zone02
EOF
check_staticroute_crd_status "example-staticroute-no-match" "nodes_shall_not_post_status"
check_route_in_container "192.168.0.0/24 via ${GW}" "all" "negative"
kubectl delete staticroute example-staticroute-no-match

fvtlog "All tests passed!"
