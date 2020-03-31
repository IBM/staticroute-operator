#!/usr/bin/env bash
set -e
set -o pipefail

SCRIPT_PATH=$PWD/$(dirname "$0")
KIND_CLUSTER_NAME="staticroute-operator-fvt"
DEBUG="${DEBUG:-false}"
# shellcheck source=scripts/fvt-tools.sh
. "${SCRIPT_PATH}/fvt-tools.sh"

cleanup() {
  fvtlog "Running cleanup, error code $?"
  if [[ "${DEBUG}" == "false" ]]; then
    kind delete cluster --name ${KIND_CLUSTER_NAME}
    rm -rf "${SCRIPT_PATH}"/kubeconfig.yaml
  fi
}

trap cleanup EXIT

fvtlog "Starting staticroute-operator fvt testing..."
kind --version || (echo "Please install kind before running fvt tests"; exit 1)

fvtlog "Creating Kubernetes cluster with kind"
if [[ "$(kind get clusters -q | grep -c ${KIND_CLUSTER_NAME})" != 1 ]]; then
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

# Get KUBECONFIG
kind get kubeconfig --name "${KIND_CLUSTER_NAME}" > "${SCRIPT_PATH}"/kubeconfig.yaml

# Get all the worker nodes
NODES=($(list_nodes))
# Restore default labels on nodes
label_nodes_with_default "zone01"

fvtlog "Loading the staticrouter operator image to the cluster..."
kind load docker-image --name="${KIND_CLUSTER_NAME}" "${REGISTRY_REPO}":"${CONTAINER_VERSION}"

fvtlog "Apply common staticoperator related resources..."
declare -a common_resources=('crds/iks.ibm.com_staticroutes_crd.yaml' 'service_account.yaml' 'role.yaml' 'role_binding.yaml');
for resource in "${common_resources[@]}"; do
  kubectl apply -f "${SCRIPT_PATH}"/../deploy/"${resource}"
done

fvtlog "Install the staticroute-operator..."
cp "${SCRIPT_PATH}"/../deploy/operator.yaml "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
sed -i "s|REPLACE_IMAGE|${REGISTRY_REPO}:${CONTAINER_VERSION}|g" "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
sed -i "s|Always|IfNotPresent|g" "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
kubectl apply -f "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
fvtlog "Check if the staticroute-operator pods are running..."
check_operator_is_running
fvtlog "OK"

fvtlog "Start applying staticroute configurations"
cat <<EOF | kubectl apply -f -
apiVersion: iks.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-simple
spec:
  subnet: "192.168.0.0/24"
---
apiVersion: iks.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-with-gateway
spec:
  subnet: "192.168.1.0/24"
  gateway: "172.17.0.3"
---
apiVersion: iks.ibm.com/v1
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
        - ${NODES[1]}
EOF

fvtlog "Check if the staticroute CRs have valid nodeStatus..."
check_staticroute_crd_status "example-staticroute-simple"
check_staticroute_crd_status "example-staticroute-with-gateway"
check_staticroute_crd_status "example-staticroute-with-selector" "${NODES[1]}"

fvtlog "Test example-staticroute-simple - Check that all workers have applied the route to 192.168.0.0/24"
check_route_in_container "192.168.0.0/24 via 172.17.0.1"

fvtlog "Test example-staticroute-with-gateway - Check that all workers have applied the route to 192.168.1.0/24 with the defined next-hop address"
check_route_in_container "192.168.1.0/24 via 172.17.0.3"

fvtlog "Test example-staticroute-with-selector - Check that only worker2 has applied the route to 192.168.2.0/24"
check_route_in_container "192.168.2.0/24 via 172.17.0.1" "${NODES[0]}" "negative"
check_route_in_container "192.168.2.0/24 via 172.17.0.1" "${NODES[1]}"

fvtlog "Test selected label is changed - worker2 has to remove its route"
kubectl label node "${NODES[1]}" kubernetes.io/hostname=temp --overwrite=true
check_staticroute_crd_status "example-staticroute-with-selector" "nodes_shall_not_post_status"
check_route_in_container "192.168.2.0/24 via 172.17.0.1" "${NODES[1]}" "negative"

fvtlog "And then apply back the label - worker2 has to restore its route"
kubectl label node "${NODES[1]}" kubernetes.io/hostname="${NODES[1]}" --overwrite=true
check_staticroute_crd_status "example-staticroute-with-selector" "${NODES[1]}"
check_route_in_container "192.168.2.0/24 via 172.17.0.1" "${NODES[1]}"

fvtlog "Test staticroute deletion - routes must be deleted"
kubectl delete staticroute example-staticroute-simple
check_route_in_container "192.168.0.0/24 via 172.17.0.1" "all" "negative"
kubectl delete staticroute example-staticroute-with-gateway
check_route_in_container "192.168.1.0/24 via 172.17.0.3" "all" "negative"
kubectl delete staticroute example-staticroute-with-selector
check_route_in_container "192.168.2.0/24 via 172.17.0.1" "${NODES[1]}" "negative"

fvtlog "Test wrong gateway configuration"
cat <<EOF | kubectl apply -f -
apiVersion: iks.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-with-wrong-gateway
spec:
  subnet: "192.168.0.0/24"
  gateway: "172.18.0.1"
EOF
check_staticroute_crd_status "example-staticroute-with-wrong-gateway" "all_nodes_shall_post_status" "network is unreachable"
check_route_in_container "192.168.0.0/24 via 172.18.0.1" "all" "negative"
kubectl delete staticroute example-staticroute-with-wrong-gateway

fvtlog "Test subnet protection"
sed -i "s|env:|env:\n        - name: PROTECTED_SUBNET_TEST1\n          value: 192.168.1.0\/24,192.168.2.0\/24|" deploy/operator.dev.yaml
sed -i "s|env:|env:\n        - name: PROTECTED_SUBNET_TEST2\n          value: 192.168.3.0\/24,192.168.4.0\/24,192.168.5.0\/24|" deploy/operator.dev.yaml
kubectl apply -f "${SCRIPT_PATH}"/../deploy/operator.dev.yaml
check_operator_is_running
cat <<EOF | kubectl apply -f -
apiVersion: iks.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-protected-subnet1
spec:
  subnet: "192.168.1.0/24"
---
apiVersion: iks.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-protected-subnet2
spec:
  subnet: "192.168.4.0/24"
EOF
check_staticroute_crd_status "example-staticroute-protected-subnet1" "all_nodes_shall_post_status" "Given subnet overlaps with some protected subnet"
check_staticroute_crd_status "example-staticroute-protected-subnet2" "all_nodes_shall_post_status" "Given subnet overlaps with some protected subnet"
check_route_in_container "192.168.1.0/24 via 172.17.0.1" "all" "negative"
check_route_in_container "192.168.4.0/24 via 172.17.0.1" "all" "negative"
kubectl delete staticroute example-staticroute-protected-subnet1 example-staticroute-protected-subnet2

fvtlog "Test staticroute when selector does not apply - nodes do not have the label, status shall be empty"
cat <<EOF | kubectl apply -f -
apiVersion: iks.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-staticroute-no-match
spec:
  subnet: "192.168.0.0/24"
  gateway: "172.18.0.1"
  selectors:
    -
      key: "failure-domain.beta.kubernetes.io/zone"
      operator: In
      values:
        - zone02
EOF
check_staticroute_crd_status "example-staticroute-no-match" "nodes_shall_not_post_status"
check_route_in_container "192.168.0.0/24 via 172.18.0.1" "all" "negative"
kubectl delete staticroute example-staticroute-no-match

fvtlog "All tests passed!"
