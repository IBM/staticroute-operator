GO111MODULE:=on
export DOCKER_BUILDKIT=1
GO_PACKAGES=$(shell go list ./... | grep -v /tests/)
GO_FILES=$(shell find . -type f -name '*.go' -not -path "./.git/*" -not -path "./api/v1/zz_generated*.go")
GOLANGCI_LINT_EXISTS:=$(shell golangci-lint --version 2>/dev/null)
GOSEC_EXISTS:=$(shell gosec --version 2>/dev/null)
GIT_COMMIT_SHA:=$(shell git rev-parse HEAD 2>/dev/null)
SHFILES=$(shell find . -type f -name '*fvt*.sh')
SHELLCHECK_EXISTS:=$(shell shellcheck --version 2>/dev/null)
YAMLLINT_EXISTS:=$(shell yamllint --version 2>/dev/null)
INSTALL_LOCATION?=$(GOPATH)/bin
MAKEFILE_DIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))

include Makefile.env
include Makefile.sdk

deps:
	GOBIN=${MAKEFILE_DIR}/bin go install sigs.k8s.io/controller-tools/cmd/controller-gen@v${CONTROLLER_GEN_VERSION}
	make _deps-$(shell uname | tr '[:upper:]' '[:lower:]')

_deps-darwin:
	$(error Operating system not supported)

_deps-linux:
	curl -sL https://github.com/operator-framework/operator-sdk/releases/download/v${OP_SDK_RELEASE_VERSION}/operator-sdk_linux_amd64 > ${INSTALL_LOCATION}/operator-sdk
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ${INSTALL_LOCATION} v${GOLANGCI_LINT_VERSION}
	curl -sL https://github.com/kubernetes-sigs/kind/releases/download/v${KIND_VERSION}/kind-linux-amd64 > ${INSTALL_LOCATION}/kind
	curl -sL https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/amd64/kubectl > ${INSTALL_LOCATION}/kubectl
	chmod +x ${INSTALL_LOCATION}/operator-sdk ${INSTALL_LOCATION}/kind ${INSTALL_LOCATION}/kubectl
	curl -sfL https://raw.githubusercontent.com/securego/gosec/master/install.sh | sh -s -- -b ${INSTALL_LOCATION} v${GOSEC_VERSION}

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

lint-yaml:
ifdef YAMLLINT_EXISTS
ifeq ($(TRAVIS),true)
	yamllint .travis.yml ./config/
endif
else
	@echo "yamllint is not installed"
endif

formatcheck:
	([ -z "$(shell gofmt -d $(GO_FILES))" ]) || (echo "Source is unformatted, please execute make format"; exit 1)

format:
	@gofmt -w ${GO_FILES}

coverage:
	go tool cover -html=cover.out -o=cover.html

sec:
ifdef GOSEC_EXISTS
	gosec -quiet ${GO_FILES}
else
	@echo "gosec is not installed"
endif

fvt: _calculate-build-number build-operator
	docker tag $(REGISTRY_REPO)-amd64 $(REGISTRY_REPO)-amd64:$(CONTAINER_VERSION)
	$(eval export REGISTRY_REPO=$(REGISTRY_REPO)-amd64)
	@scripts/run-fvt.sh

validate-code: lint lint-sh lint-yaml formatcheck vet sec test

update-operator-resource:
	make manifests

build-operator: update-operator-resource validate-code
	make docker-build IMG=$(REGISTRY_REPO)

dev-publish-image: _calculate-build-number build-operator
	docker tag $(REGISTRY_REPO) $(REGISTRY_REPO):$(CONTAINER_VERSION)
	docker push $(REGISTRY_REPO):$(CONTAINER_VERSION)
	@echo "\n image: $(REGISTRY_REPO):$(CONTAINER_VERSION)"

dev-run-operator-local: dev-apply-common-resources
	# pick the first node to test run
	$(eval export NODE_HOSTNAME=$(shell sh -c "kubectl get nodes -o jsonpath='{ $$.items[0].status.addresses[?(@.type==\"Hostname\")].address }'"))
	make run

dev-run-operator-remote: dev-publish-image dev-apply-common-resources
	cat config/manager/manager.yaml | sed 's|REPLACE_IMAGE|$(REGISTRY_REPO):$(CONTAINER_VERSION)|g' > config/manager/manager.dev.yaml
	kubectl create -f config/manager/manager.dev.yaml || :

dev-apply-common-resources:
	kubectl create -f config/crd/bases/static-route.ibm.com_staticroutes.yaml || :
	kubectl create -f config/rbac/service_account.yaml || :
	kubectl create -f config/rbac/role.yaml || :
	kubectl create -f config/rbac/role_binding.yaml || :

dev-cleanup-operator:
	kubectl delete -f config/crd/bases/static-route.ibm.com_staticroutes.yaml || :
	kubectl delete -f config/manager/manager.dev.yaml || :
	kubectl delete -f config/rbac/role.yaml || :
	kubectl delete -f config/rbac/role_binding.yaml || :
	kubectl delete -f config/rbac/service_account.yaml || :
