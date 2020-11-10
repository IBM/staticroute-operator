GO111MODULE:=on
export DOCKER_BUILDKIT=1
GO_PACKAGES=$(shell go list ./... | grep -v /tests/)
GO_FILES=$(shell find . -type f -name '*.go' -not -path "./.git/*")
GOLANGCI_LINT_EXISTS:=$(shell golangci-lint --version 2>/dev/null)
GIT_COMMIT_SHA:=$(shell git rev-parse HEAD 2>/dev/null)
SHFILES=$(shell find . -type f -name '*fvt*.sh')
SHELLCHECK_EXISTS:=$(shell shellcheck --version 2>/dev/null)
INSTALL_LOCATION?=$(GOPATH)/bin

include Makefile.env

deps:
	make _deps-$(shell uname | tr '[:upper:]' '[:lower:]')

_deps-darwin:
	$(error Operating system not supported)

_deps-linux:
	curl -sL https://github.com/operator-framework/operator-sdk/releases/download/v${OP_SDK_RELEASE_VERSION}/operator-sdk-v${OP_SDK_RELEASE_VERSION}-x86_64-linux-gnu > ${INSTALL_LOCATION}/operator-sdk
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ${INSTALL_LOCATION} v${GOLANGCI_LINT_VERSION}
	curl -sL https://github.com/kubernetes-sigs/kind/releases/download/v${KIND_VERSION}/kind-linux-amd64 > ${INSTALL_LOCATION}/kind
	curl -sL https://storage.googleapis.com/kubernetes-release/release/v${KUBECTL_VERSION}/bin/linux/amd64/kubectl > ${INSTALL_LOCATION}/kubectl
	chmod +x ${INSTALL_LOCATION}/operator-sdk ${INSTALL_LOCATION}/kind ${INSTALL_LOCATION}/kubectl

_calculate-build-number:
    $(eval export CONTAINER_VERSION?=$(GIT_COMMIT_SHA)-$(shell date "+%s"))

lint:
ifdef GOLANGCI_LINT_EXISTS
	golangci-lint run --verbose --timeout 3m
else
	@echo "golangci-lint is not installed"
endif

lint-sh:
ifdef SHELLCHECK_EXISTS
	shellcheck ${SHFILES}
else
	@echo "shellcheck is not installed"
endif

formatcheck:
	([ -z "$(shell gofmt -d $(GO_FILES))" ]) || (echo "Source is unformatted, please execute make format"; exit 1)

format:
	@gofmt -w ${GO_FILES}

coverage:
	go tool cover -html=cover.out -o=cover.html

vet:
	go vet ${GO_PACKAGES}

test:
	go test -race -timeout 60s -covermode=atomic -coverprofile=cover.out ${GO_PACKAGES}

fvt: _calculate-build-number build-operator
	docker tag $(REGISTRY_REPO) $(REGISTRY_REPO):$(CONTAINER_VERSION)
	$(eval export REGISTRY_REPO?=$(REGISTRY_REPO))
	@scripts/run-fvt.sh

validate-code: lint lint-sh formatcheck vet test

update-operator-resource:
	operator-sdk generate crds
	operator-sdk generate k8s

build-operator: update-operator-resource validate-code
	operator-sdk build $(REGISTRY_REPO)

dev-publish-image: _calculate-build-number build-operator
	docker tag $(REGISTRY_REPO) $(REGISTRY_REPO):$(CONTAINER_VERSION)
	docker push $(REGISTRY_REPO):$(CONTAINER_VERSION)
	@echo "\n image: $(REGISTRY_REPO):$(CONTAINER_VERSION)"

dev-run-operator-local: build-operator dev-apply-common-resources
	# pick the first node to test run
	$(eval export NODE_HOSTNAME=$(shell sh -c "kubectl get nodes -o jsonpath='{ $$.items[0].status.addresses[?(@.type==\"Hostname\")].address }'")) 
	operator-sdk run --local --namespace=default --kubeconfig=$(KUBECONFIG)

dev-run-operator-remote: dev-publish-image dev-apply-common-resources
	cat deploy/operator.yaml | sed 's|REPLACE_IMAGE|$(REGISTRY_REPO):$(CONTAINER_VERSION)|g' > deploy/operator.dev.yaml
	kubectl create -f deploy/operator.dev.yaml  || :

dev-apply-common-resources:
	kubectl create -f deploy/crds/static-route.ibm.com_staticroutes_crd.yaml || :
	kubectl create -f deploy/service_account.yaml  || :
	kubectl create -f deploy/role.yaml  || :
	kubectl create -f deploy/role_binding.yaml  || :

dev-cleanup-operator:
	kubectl delete -f deploy/crds/static-route.ibm.com_staticroutes_crd.yaml  || :
	kubectl delete -f deploy/operator.dev.yaml  || :
	kubectl delete -f deploy/role.yaml  || :
	kubectl delete -f deploy/role_binding.yaml  || :
	kubectl delete -f deploy/service_account.yaml  || :
