#!/bin/bash
set -e
set -o pipefail

SCRIPT_PATH=$PWD/$(dirname "$0")
KIND_CLUSTER_NAME="staticroute-operator-fvt"
DEBUG="false"

# shellcheck source=scripts/fvt-tools.sh
. "${SCRIPT_PATH}/fvt-tools.sh"

cleanup() {
  fvtlog "Running cleanup, error code $?"
  if [[ "${DEBUG}" == "false" ]]; then
    kind delete cluster --name ${KIND_CLUSTER_NAME}
  fi
}

trap cleanup EXIT

fvtlog "Starting staticroute-operator fvt testing..."
kind --version || (echo "Please install kind before running fvt tests"; exit 1)

fvtlog "Creating Kubernetes cluster with kind"
if [[ "$(kind get clusters -q | grep -c ${KIND_CLUSTER_NAME})" -ne 1 ]]; then
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
list_nodes

fvtlog "Loading the staticrouter operator image to the cluster..."
kind load docker-image --name="${KIND_CLUSTER_NAME}" "${REGISTRY_REPO}":"${CONTAINER_VERSION}"

fvtlog "Apply common staticoperator related resources..."
kubectl apply -f "${SCRIPT_PATH}"/../deploy/crds/iks.ibm.com_staticroutes_crd.yaml
kubectl apply -f "${SCRIPT_PATH}"/../deploy/service_account.yaml
kubectl apply -f "${SCRIPT_PATH}"/../deploy/role.yaml
kubectl apply -f "${SCRIPT_PATH}"/../deploy/role_binding.yaml

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
        - ${KIND_CLUSTER_NAME}-worker2
EOF

fvtlog "Check if the staticroute CRs have valid nodeStatus..."
check_staticroute_crd_status "example-staticroute-simple"
check_staticroute_crd_status "example-staticroute-with-gateway"
check_staticroute_crd_status "example-staticroute-with-selector" "${KIND_CLUSTER_NAME}-worker2"

fvtlog "Test 01 - Check that all workers have applied the route to 192.168.0.0/24"
check_route_in_container "192.168.0.0/24 via 172.17.0.1"

fvtlog "Test 02 - Check that all workers have applied the route to 192.168.1.0/24 with the defined next-hop address"
check_route_in_container "192.168.1.0/24 via 172.17.0.3"

fvtlog "Test 03 - Check that only worker2 has applied the route to 192.168.2.0/24"
check_route_in_container "192.168.2.0/24 via 172.17.0.1" "${KIND_CLUSTER_NAME}-worker" "negative"
check_route_in_container "192.168.2.0/24 via 172.17.0.1" "${KIND_CLUSTER_NAME}-worker2"

fvtlog "All tests passed!"
