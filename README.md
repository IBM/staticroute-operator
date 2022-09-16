[![Go Report Card](https://goreportcard.com/badge/github.com/IBM/staticroute-operator)](https://goreportcard.com/report/github.com/IBM/staticroute-operator) [![Active](http://img.shields.io/badge/Status-Active-green.svg)](https://github.com/IBM/staticroute-operator) [![PR's Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat)](https://github.com/IBM/staticroute-operator/pulls) [![Build Status](https://travis-ci.com/IBM/staticroute-operator.svg?branch=master)](https://travis-ci.com/IBM/staticroute-operator) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0) [![Code of Conduct](https://img.shields.io/badge/code%20of-conduct-ff69b4.svg?style=flat)](https://www.ibm.com/partnerworld/program/code-of-conduct) 

# static-route-operator
Static IP route operator for Kubernetes clusters

This project is under development, use it on your own risk please.

# Usage

Public OCI images are not available yet. To give a try to the project you have to build your own image and store it in your image repository. Please follow some easy steps under `Development` section of the page.
After build you have to apply some Kubernetes manifests: `config/crd/bases/static-route.ibm.com_staticroutes.yaml`, `config/rbac/service_account.yaml`, `config/rbac/role.yaml`, `config/rbac/role_binding.yaml` and `config/manager/manager.dev.yaml`.
Finaly you have to create `StaticRoute` custom resource on the cluster. The operator will pick it up and creates underlaying routing policies based on the given resource.

## Sample custom resources

Route a subnet across the default gateway.
```
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route
spec:
  subnet: "192.168.0.0/24"
```

Route a subnet to the custom gateway.
```
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route
spec:
  subnet: "192.168.0.0/24"
  gateway: "10.0.0.1"
```

Selecting target node(s) of the static route by label(s):
```
apiVersion: static-route.ibm.com/v1
kind: StaticRoute
metadata:
  name: example-static-route-with-selector
spec:
  subnet: "192.168.1.0/24"
  selectors:
    -
      key: "kubernetes.io/arch"
      operator: In
      values:
        - "amd64"
```

## Runtime customizations of operator

 * Routing table: By default static route controller uses #254 table to configure static routes. The table number is configurable by giving a valid number between 0 and 254 as `TARGET_TABLE` environment variable. Changing the target table on a running operator is not supported. You have to properly terminate all the existing static routes by deleting the custom resources before restarting the operator with the new config.
 * Protect subnets: Static route operator allows to set any subnet as routing destination. In some cases users can break the entire network by mistake. To protect some of the subnets you can use a comma separated list in environment variables starting with the string `PROTECTED_SUBNET_` (ie. `PROTECTED_SUBNET_CALICO=172.0.0.1/24,10.0.0.1/24`). The operator will ignore custom route if the subnets (in the custom resource and the protected list) are overlapping each other.
 * Fallback IP address for GW selection: if the gateway parameter is not provided in any CR, static route operator will select the gateway based on a predefined IP address (NOT CIDR). The address can be provided via an environment variable: `FALLBACK_IP_FOR_GW_SELECTION`. If the environment variable is not provided for the operator, it will use `10.0.0.1` as a default value.

# Development

## Prerequisites
The following components are needed to be installed on your environment:
  * git
  * go 1.18+
  * docker
  * kubectl v1.22.2 or newer
  * KinD v0.11.1 (for testing)
  * golangci-lint v1.49.0
  * Operator SDK CLI 1.23.0 (more information: https://sdk.operatorframework.io/docs/installation/)
  * and access to a Kubernetes cluster on a version v1.21.0 or newer
  * before you run any of the make target below, make sure the following are done:
    - export `REGISTRY_REPO` environment variable to your docker registry repo url (ie.: quay.io/example/static-route-operator:v0.0.1)
    - export `KUBECONFIG` environment variable to the path of kubeconfig file (if not set, default $$HOME/.kube/config will be used)
    - login to your docker registry using your credentials (ie.: docker login... , ibmcloud cr login etc.)

## Changing Go build version
You can change the builder Go version for Static Route operator in `Makefile.env`. Please note, that since the docker build is done inside separately in a Go builder image (`GO_BUILDER_IMAGE`), you should also change Travis Go version in `.travis.yaml`, to make sure that the Go tests are running on the same Go version as the build.
## Updating the Custom Resource Definitions (CRDs)
Make sure, that every time you modify anything in `*_types.go` file, run the `make generate` (DeepCopy, DeepCopyInto, and DeepCopyObject...) and `make manifests` (WebhookConfiguration, ClusterRole and CustomResourceDefinition...) to update generated code for `k8s` and `CRDs`.

## Building the static route operator
`make deps` it is strongly recommended to run this make target before trying to build the operator.
`make build-operator` target can be used for updating, building operator. It executes all the static code analyzing.
`make dev-publish-image` publishes a new build of the operator image into your Docker repository.

## Testing the changes
Once you have made changes in the source, you have two option to run and test your operator:
- as a `deployment` inside a Kubernetes cluster
- as a binary program running locally on your development environment
  1. Run as a deployment inside your cluster
     - run the `make dev-run-operator-remote` target which updates your operator resources, builds the operator, pushes the built operator docker image to the `REGISTRY_REPO`, changes the operator manifest file and creates the Kubernetes resources (CRDs, operator, role, rolebinding and service account) inside the cluster
     - you can remove the operator resources using `make dev-cleanup-operator` target
  2. Run as a Go program on your local development environment
     - run `make dev-run-operator-local`

## Functional verification tests
The fvt tests are written is bash and you could find it under the `scripts` directory. By default it uses the [KinD](https://kind.sigs.k8s.io/docs/user/quick-start/) environment to setup a Kubernetes cluster and then it applies all the needed resources and starts the operator.
  - run `make fvt` to execute the functional tests

Please note, the fvt test currently does not check network connectivity, it only makes sure that the relevant and necessary routes are setup on the node (container). Travis also runs these tests.

Also there is an option to functionally test the operator on an existing cluster (in a cloud or in on-premise) by customizing test run with environment variables. The only prerequisite is that you shall access your cluster via `kubectl` commands before running the tests.
  - set the Prerequisites described above (repo name, kube config, docker login etc.)
  - export the following environment variables depending on your needs
    - PROVIDER (can be `ibmcloud`, if not set then KinD will be used)
    - SKIP_OPERATOR_INSTALL (if you already have an operator, set this to `true`. Default is `false`)
    - PROTECTED_SUBNET_TEST1
    - PROTECTED_SUBNET_TEST2 (list of protected subnets to test, if either of them are empty then no protected subnet test will run)

## Setting Travis-CI
If you want to test, build and publish your changes into your own personal repo after forking this project, you need to following variables set up in Travis instance associated to your github project:
  - DOCKER_IMAGE_NAME, this is the name of your docker image ie. myrepo/staticroute-operator
  - DOCKER_REGISTRY_LIST, you need at least one docker repository url to publish docker images. This is a comma separated list of repo urls.
  - DOCKER_USERNAME, username for your docker repository
  - GH_REPO, your github repo with the project name, ie: github.com/myrepo/staticroute-operator
  - GH_TOKEN, github token generated to access (tag, and push) to your github repository
  - and a set of variables that contains the docker password for each repository url ie. if you set `my.docker.repo.io,quay.io` in DOCKER_REGISTRY_LIST than you need a `my_docker_repo_io` and `quay_io` secrets with the corresponding passwords
  (Note: you should take care of GH_TOKEN and docker passwords to be non-visible secrets in Travis!)

# Contributing

We appreciate your help!

To contribute, please read our contribution guidelines: [CONTRIBUTION.md](CONTRIBUTION.md)

Note that the Static Route Operator project uses the [issue tracker](https://github.com/IBM/staticroute-operator/issues) for bug reports and proposals only. If you have questions, engage our team via Slack by [registering here](https://cloud.ibm.com/kubernetes/slack) and join the discussion in the #general channel on our [public IBM Cloud Kubernetes Service Slack](https://ibm-cloud-success.slack.com/). 
