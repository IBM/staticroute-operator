#!/usr/bin/env bash
set -e
set -o pipefail

SCRIPT_PATH=$PWD/$(dirname "$0")
KIND_CLUSTER_NAME="static-route-operator-fvt"
KIND_IMAGE_VERSION="kindest/node:v1.29.2@sha256:51a1434a5397193442f0be2a297b488b6c919ce8a3931be0ce822606ea5ca245"
KEEP_ENV="${KEEP_ENV:-false}"
SKIP_OPERATOR_INSTALL="${SKIP_OPERATOR_INSTALL:-false}"
PROVIDER="${PROVIDER:-kind}"
FVT_HELPER_IMAGE="${FVT_HELPER_IMAGE:-busybox}"
IMAGEPULLSECRET="${IMAGEPULLSECRET:-}"

# shellcheck source=scripts/fvt-tools.sh
. "${SCRIPT_PATH}/fvt-tools.sh"

cleanup() {
  fvtlog "Running cleanup, error code $?"
  delete_hostnet_pods
  if [[ "${KEEP_ENV}" == "false" ]]; then
    kubectl delete staticroute --all &>/dev/null
    if [[ "${SKIP_OPERATOR_INSTALL}" == "false" ]]; then
      manage_common_operator_resources "delete"
    fi
    pod_security_unlabel_namespace default
    if [[ "${PROVIDER}" == "kind" ]]; then
      kind delete cluster --name ${KIND_CLUSTER_NAME}
      rm -rf "${SCRIPT_PATH}"/kubeconfig.yaml
    fi
  fi
}

trap cleanup EXIT

fvtlog "Preparing environment for static-route-operator tests..."

## Prepare the environment for testing
if [[ ${PROVIDER} == "kind" ]]; then
    fvtlog "Provider is set to KinD, creating cluster..."
    create_kind_cluster

    # Get KUBECONFIG
    kind get kubeconfig --name "${KIND_CLUSTER_NAME}" > "${SCRIPT_PATH}"/kubeconfig.yaml

    fvtlog "Loading the static-route-operator image to the cluster..."
    kind load docker-image --name="${KIND_CLUSTER_NAME}" "${REGISTRY_REPO}":"${CONTAINER_VERSION}"
else
    fvtlog "Provider was set to ${PROVIDER}, use the provided cluster."
fi

# Label default namespace to allow privliged pod creation
pod_security_label_namespace default

# Support for manual install
if [[ "${SKIP_OPERATOR_INSTALL}" == false ]]; then
    manage_common_operator_resources "apply"
fi

# Get all the worker nodes
update_node_list

# Spin up helper pods to exec onto node network NS.
create_hostnet_pods

# Delete all the static routes before the test
kubectl delete staticroute --all &>/dev/null

# Restore default labels on nodes
label_nodes_with_default "zone01"

## start the actual tests
fvtlog "Starting static-route-operator fvt testing..."
fvtlog "Check if the static-route-operator pods are running..."
check_operator_is_running
fvtlog "OK"

# Choose a node to test selector case
A_NODE=$(pick_non_master_node)

# Get default gateway on selected node
GW=$(get_default_gw "${A_NODE}")
fvtlog "Nodes: ${NODES[*]}"
fvtlog "Choosing Gateway: ${GW}"
fvtlog "Choosing K8s node as selector tests: ${A_NODE}"

fvtlog "Start applying static-route configurations"

if [[ ${PROVIDER} == "kind" ]]; then
  cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-simple
spec:
  subnet: "192.168.0.0/24"
---
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-with-gateway
spec:
  subnet: "192.168.1.0/24"
  gateway: "${GW}"
EOF
fi

cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-with-selector
spec:
  subnet: "192.168.2.0/24"
  selectors:
    -
      key: "kubernetes.io/hostname"
      operator: In
      values:
        - ${A_NODE}
EOF

fvtlog "Check if the static-route CRs have valid nodeStatus..."

if [[ ${PROVIDER} == "kind" ]]; then
  check_static_route_crd_status "example-static-route-simple"
  check_static_route_crd_status "example-static-route-with-gateway"
fi

check_static_route_crd_status "example-static-route-with-selector" "${A_NODE}"

if [[ ${PROVIDER} == "kind" ]]; then
  fvtlog "Test example-static-route-simple - Check that all workers have applied the route to 192.168.0.0/24"
  check_route_on_nodes "192.168.0.0/24 via ${GW}"

  fvtlog "Test example-static-route-with-gateway - Check that all workers have applied the route to 192.168.1.0/24 with the defined next-hop address"
  check_route_on_nodes "192.168.1.0/24 via ${GW}"
fi

fvtlog "Test example-static-route-with-selector - Check that only ${A_NODE} has applied the route to 192.168.2.0/24"
for index in ${!NODES[*]}
do
  if [[ "${A_NODE}" == "${NODES[$index]}" ]]; then
    check_route_on_nodes "192.168.2.0/24 via ${GW}" "${NODES[$index]}"
  else
    check_route_on_nodes "192.168.2.0/24 via ${GW}" "${NODES[$index]}" "default" "negative"
  fi
done

fvtlog "Test selected label is changed - ${A_NODE} has to remove its route"
kubectl label node "${A_NODE}" kubernetes.io/hostname=temp --overwrite=true
check_static_route_crd_status "example-static-route-with-selector" "nodes_shall_not_post_status"
check_route_on_nodes "192.168.2.0/24 via ${GW}" "${A_NODE}" "default" "negative"

fvtlog "And then apply back the label - ${A_NODE} has to restore its route"
kubectl label node "${A_NODE}" kubernetes.io/hostname="${A_NODE}" --overwrite=true
check_static_route_crd_status "example-static-route-with-selector" "${A_NODE}"
check_route_on_nodes "192.168.2.0/24 via ${GW}" "${A_NODE}"

# since testing node remove/re-add is bit slow and complicated on a real cluster
# this just goes with KinD (default) provider
if [[ ${PROVIDER} == "kind" ]]; then
  fvtlog "Test node failure. Other node shall cleanup the status on behalf of the failed node"
  DELETED_NODE_NAME="${A_NODE}"
  DELETED_NODE_MANIFEST="$(kubectl get no "${A_NODE}" -o yaml)"

  docker pause "${A_NODE}"
  kubectl delete no "${A_NODE}"

  update_node_list
  check_static_route_crd_status "example-static-route-simple"
  check_static_route_crd_status "example-static-route-with-gateway"
  check_static_route_crd_status "example-static-route-with-selector" "nodes_shall_not_post_status"
  fvtlog "Restore failed node. It shall catch up again with the routes"
  echo "${DELETED_NODE_MANIFEST}" | kubectl apply -f -
  docker unpause "${DELETED_NODE_NAME}"

  update_node_list
  check_static_route_crd_status "example-static-route-simple"
  check_static_route_crd_status "example-static-route-with-gateway"
  check_static_route_crd_status "example-static-route-with-selector" "${A_NODE}"
else
  fvtlog "Provider is not KinD, skipping node failure tests"
fi

update_node_list
fvtlog "Test static-route deletion - routes must be deleted"
if [[ ${PROVIDER} == "kind" ]]; then
  kubectl delete staticroute example-static-route-simple
  check_route_on_nodes "192.168.0.0/24 via ${GW}" "all" "default" "negative"
  kubectl delete staticroute example-static-route-with-gateway
  check_route_on_nodes "192.168.1.0/24 via ${GW}" "all" "default" "negative"
fi

kubectl delete staticroute example-static-route-with-selector
check_route_on_nodes "192.168.2.0/24 via ${GW}" "all" "default" "negative"

fvtlog "Test wrong gateway configuration"
cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-with-wrong-gateway
spec:
  subnet: "192.168.0.0/24"
  gateway: "1.2.3.4"
EOF
check_static_route_crd_status "example-static-route-with-wrong-gateway" "all_nodes_shall_post_status" "Given gateway IP is not directly routable, cannot setup the route"
check_route_on_nodes "192.168.0.0/24 via 1.2.3.4" "all" "default" "negative"
kubectl delete staticroute example-static-route-with-wrong-gateway

if [[ "${PROVIDER}" == "kind" ]]; then
  fvtlog "Test subnet protection (KinD)"
  sed -i "s|env:|env:\n        - name: PROTECTED_SUBNET_TEST1\n          value: 192.168.1.0\/24,192.168.2.0\/24|" config/manager/manager.dev.yaml
  sed -i "s|env:|env:\n        - name: PROTECTED_SUBNET_TEST2\n          value: 192.168.3.0\/24,192.168.4.0\/24,192.168.5.0\/24|" config/manager/manager.dev.yaml
  SUBNET1="192.168.1.0/24"
  SUBNET2="192.168.4.0/24"
  kubectl apply -f "${SCRIPT_PATH}"/../config/manager/manager.dev.yaml
else
  fvtlog "Test subnet protection"
  if [[ "${PROTECTED_SUBNET_TEST1}" && "${PROTECTED_SUBNET_TEST2}" ]]
  then
    kubectl get ds static-route-operator -nkube-system -oyaml | \
      sed "s|- env:|- env:\n        - name: PROTECTED_SUBNET_TEST1\n          value: ${PROTECTED_SUBNET_TEST1}\n        - name: PROTECTED_SUBNET_TEST2\n          value: ${PROTECTED_SUBNET_TEST2}|" > operator.yaml
      cat operator.yaml
      kubectl apply -f operator.yaml

    SUBNET1=$(pick_protected_subnet "${PROTECTED_SUBNET_TEST1}")
    SUBNET2=$(pick_protected_subnet "${PROTECTED_SUBNET_TEST2}")
  else
    fvtlog "No subnet env variables, skipping subnet protection test"
    SKIP_PROTECTED_SUBNET_TESTS=true
  fi
fi

check_operator_is_running

if [[ ! "${SKIP_PROTECTED_SUBNET_TESTS}" ]]; then
  cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-protected-subnet1
spec:
  subnet: "${SUBNET1}"
---
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-protected-subnet2
spec:
  subnet: "${SUBNET2}"
EOF
  update_node_list
  check_static_route_crd_status "example-static-route-protected-subnet1" "all_nodes_shall_post_status" "Given subnet overlaps with some protected subnet"
  check_static_route_crd_status "example-static-route-protected-subnet2" "all_nodes_shall_post_status" "Given subnet overlaps with some protected subnet"
  check_route_on_nodes "${SUBNET1} via ${GW}" "all" "default" "negative"
  check_route_on_nodes "${SUBNET2} via ${GW}" "all" "default" "negative"
  kubectl delete staticroute example-static-route-protected-subnet1 example-static-route-protected-subnet2
fi

if [[ ${PROVIDER} == "kind" ]]; then
  fvtlog "Test static-route when selector does not apply - nodes do not have the label, status shall be empty"
  cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-no-match
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
  check_static_route_crd_status "example-static-route-no-match" "nodes_shall_not_post_status"
  check_route_on_nodes "192.168.0.0/24 via ${GW}" "all" "default" "negative"
  kubectl delete staticroute example-static-route-no-match
fi

if [[ ${PROVIDER} == "kind" ]]; then
  fvtlog "Test using a different routing table - route should appear in table 30"
  cat <<EOF | kubectl apply -f -
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-different-table
spec:
  subnet: "192.168.0.0/24"
  gateway: "${GW}"
  table: 30
EOF
  check_static_route_crd_status "example-static-route-different-table"
  check_route_on_nodes "192.168.0.0/24 via ${GW}" "all" "30"
  kubectl delete staticroute example-static-route-different-table
fi

fvtlog "All tests passed!"
