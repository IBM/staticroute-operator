REGISTRY_REPO?=quay.io/example/staticroute-operator
KUBECONFIG?=$$HOME/.kube/config
GIT_COMMIT_SHA:=$(shell git rev-parse HEAD 2>/dev/null)

_calculate-build-number:
    $(eval export CONTAINER_VERSION?=$(GIT_COMMIT_SHA)-$(shell date "+%s"))

update-operator-resource:
	operator-sdk generate crds
	operator-sdk generate k8s

build-operator: update-operator-resource
	operator-sdk build $(REGISTRY_REPO)

dev-publish-image: _calculate-build-number build-operator
	docker tag $(REGISTRY_REPO) $(REGISTRY_REPO):$(CONTAINER_VERSION)
	docker push $(REGISTRY_REPO):$(CONTAINER_VERSION)
	@echo "\n image: $(REGISTRY_REPO):$(CONTAINER_VERSION)"

dev-run-operator-local: build-operator
	# pick the first node to test run
	$(eval export NODE_HOSTNAME=$(shell sh -c "kubectl get nodes -o jsonpath='{ $$.items[0].status.addresses[?(@.type==\"Hostname\")].address }'")) 
	operator-sdk run --local --namespace=default --kubeconfig=$(KUBECONFIG)

dev-run-operator-remote: dev-publish-image
	cat deploy/operator.yaml | sed 's|REPLACE_IMAGE|$(REGISTRY_REPO)|g' > deploy/operator.dev.yaml
	kubectl create -f deploy/crds/iks.ibm.com_staticroutes_crd.yaml || :
	kubectl create -f deploy/service_account.yaml  || :
	kubectl create -f deploy/role.yaml  || :
	kubectl create -f deploy/role_binding.yaml  || :
	kubectl create -f deploy/operator.dev.yaml  || :
	kubectl create -f deploy/crds/iks.ibm.com_v1_staticroute_cr.yaml  || :

dev-cleanup-operator:
	kubectl delete -f deploy/crds/iks.ibm.com_v1_staticroute_cr.yaml  || :
	kubectl delete -f deploy/operator.dev.yaml  || :
	kubectl delete -f deploy/role.yaml  || :
	kubectl delete -f deploy/role_binding.yaml  || :
	kubectl delete -f deploy/service_account.yaml  || :
	kubectl delete -f deploy/crds/iks.ibm.com_staticroutes_crd.yaml  || :
