# [WIP] Software design document
... for managing static routes on Kubernetes clusters with an in-cluster operator.

## Disclaimer
The solution below is assuming the cluster administrator has a good understanding of the cluster network setup and will prevent only a limited amount of user errors.

Also, the solution is not intended to provide the basic cluster network setup (i.e. API server or container registry reachability) as it would cause chicken-egg problems.

## Terms
| Term | Explanation                |
|------|----------------------------|
| IKS  | IBM Kubernetes Service     |
| CRD  | Custom Resource Definition |
| CR   | Custom Resource (instance) |
| DS   | DaemonSet                  |

## Concept
There are use-cases when Kubernetes cluster administrator wants to manage custom static routes on the cluster member nodes. Such use case can be i.e. connecting clusters in the cloud to customer's private on-prem datacenters/servers via some VPN solution.

Assuming:
* An (on-prem) application sends a request towards a Pod in the Kubernetes cluster
* And the networking solution preserves the original (on-prem) source IP

In this case Reverse Path Filtering (uRPF) will drop the response on the worker as the original (on-prem) source IP (as a destination) is not routable according to the Kubernetes Node's routing tables currently.

The solution is to allow customers to create custom static IP routes on all (or on selected) Kubernetes nodes. Taking the VPN example, such static routes would point to the VPN gateway as next hop if the destination IP range falls into one of the selected customer's on-prem datacenter's range.

The current solution is based on other existing solutions.
There are existing (customer specific) solutions for the same, created by IBM colleagues, like: https://github.com/jkwong888/k8s-add-static-routes and https://github.com/jkwong888/iks-overlay-ip-controller

The solution is deployed in a DaemonSet on the entire cluster. The deployed Pods will have a single controller loop to manage the IP routes locally on the Node, instructed by a Custom Resource, managed by the user. There is no central entity, who manages the DS Pods, they are all equal and independently watching the Custom Resource(s).

### References:
* Original CoreOS blogpost about operators: https://coreos.com/blog/introducing-operators.html
* Operator-SDK resources: https://github.com/operator-framework/operator-sdk

## Configuration options
### Node selection
If the user is willing to specify the static route only for a subset of Nodes, it is possible via arbitrary label selectors in the CR. Some examples assuming the cluster is IBM Cloud Kubernetes Service:
* Horizontal Node selection by specifying the worker-pool
* Vertical Node selection by specifying the compute region

### Black-list of subnets
In order to avoid user error (i.e. lock-out and/or isolate the node(s)), there shall be a predefined list of subnets, which is immutable during runtime and contains subnets, which are forbidden to use for route creation. The default list in the example manifest files are set to work with IKS.

### Route table selection
One may want to manage the subject IP routes in a way that they are created in a custom route table, instead of the default. This is useful when the default route table is managed by some other network management solution. By default, the main routing table is used.

### Fall-back subnet for gateway selection
When CR omits the IP of the gateway, the controller is able to dynamically detect the GW which is used on the nodes, though this is not guaranteed to work in all cases. The detection is based on a subnet specified by this option. By default it is `10.0.0.0/8`.

### Tamper reaction
TODO: decide if this is needed. The option might set whether the destroyed route shall be recreated (with a timeout) or only the reporting of the problem is needed.

## Required authorizations
The Pods need to watch and update the CR instances. Also, the in order to react on node loss, the Pods need to watch Nodes.

As the Pods are modifying the node's IP stack configuration, they need to have NETADMIN capability and host networking.

## Components, external packages
* The main component is the [Operator SDK](https://github.com/operator-framework/operator-sdk/). It is used to generate/update the skeleton of the project and the CRD/CR. The second line dependecies, requires by the SDK (such as client-go for Kubernetes) are not listed here.
* Go-lang [netlink](https://github.com/vishvananda/netlink) interface to manage IP routes, and also capture unintended IP route changes.

## CRD content
### Specification
Fields in `.spec`:
* Subnet: string representation of the desired subnet to route. Format: x.x.x.x/x (example: 192.168.1.0/24)
* Gateway: IP address of the gateway as the next hop for the subnet. Can be empty.

### Status
As there is no central entity, all Pod running on the Nodes are responsible to update the status in the CR. As a result, the `.status` sub-resource is a list of individual node statuses.
TODO decide to report the `generation` field or the CR content in status.

### Finalizers
There is a single common finalizer used in the CR which is managed by the Pods. The finalizer is immediately put on the CR after creation by the fastest controller (DS Pod). This will prevent the deletion of the CR until all Pod cleaned up the IP routes on the nodes. After the user is asked to delete the CR (`kubectl delete ...`), the Pods are in charge to remove themselves from the `.status` if they are ready with the deletion of the IP route. When the `.status` is empty, the fastest Pod will remove the finalizer and the CR will be removed by the API-server.
Due to the API-server concurrency handling (using `resourceVersion`), there is no need to have any leader to do the finalizer task.

## Feedback to the user
The main feedback to the user is the `.status` sub-resource of the CR. It is always updated with the Node statuses, when they create/update/delete the route according to the CR.
TODO: decide whether custom Node events are also needed or not.

## Concurrency management
Kubernetes API uses so-called optimistic concurrency. That means the API-server is applying server-side logic and not accepting object changes blindly. The clients which are acting on the same resource does not have to coordinate their write attempts. The API-server will gracefully deny any write operation if the write is not targeting the latest object version. This is controlled by the `resourceVersion` metadata. The client, however is required to re-fetch the most recent object version and re-compute it's change in case when the write fails. Operator SDK follows this requirement by re-injecting the reconciliation event to the controller when error reported in the previous round. Controller code is in charge to report such write error to the SDK. With large clusters, this might happen multiple times, until every Pod is able to update the status and finished the reconciliation.

The same behavior applies to every sub-resources of the objects (`.spec`, `.status`, etc.).

More on the topic [here](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#concurrency-control-and-consistency)

## Failure scenarios and recovery
### Controller Pod restarts
Operator SDK is responsible to inject reconciliation requests for all existing CRs on startup. The controller code shall use this opportunity to catch up with all the events which happened during downtime.

### Node scaling or deletion
If a node is deleted or destroyed in a way that it could not clean up it's routes, and more importantly the `.status` in the CRs, it would prevent the deletion of the CR. To overcome on this, there is a dedicated control loop in the Pods with a leader elected, who is listening any node deletion and clean up the `.status` for them in the CRs if it didn't happen.

### Tamper detection
It might happen that an already created IP route is destroyed by another entity. This can be either the user itself or another controller mechanism on the node. Linux kernel offers an event source (netlink) to detect IP stack changes, so the controller is able to detect, report and react on the changes.

## Controller loops
### Static route controller, CR watcher
This is the main functionality. It is based on a generated controller by Operator SDK. This controller is running in all-active. This means there is no leader election, every node runs it's instance, which is realizing the routes on the node according to the CR and reporting back to the CR's `.status`. This controller is contacting the static route manager (see below) to realize the route changes.

The code is under `pkg/controller/staticroute/staticroute_controller.go` and the data types are under `pkg/apis/iks/v1/staticroute_types.go`.

### Node cleaner
If any node is terminated and deleted from Kubernetes API, it can happen that the respective `.status` field is not cleaned up by the operator instance, which was running on the node. This blocks the CR deletion, since the finalizer will be removed only when the `.status` sub-resource is empty (which means all operator instance clean up the IP route in the kernel). This is a known edge case and needs to have graceful handling.

The node cleaner is a second controller loop running in all operator instances. However, it is sufficient to have only a single active instance in the cluster, which means this controller loop shall run with leader election. It reconciles the core Node objects. When a DELETE action is happening, it scans through the current CRs and cleans up the leftover `.status` entries instead of the retired node (if exists). When leader election happens, the full review of the Nodes and CRs are performed to catch up with any missing events.

TODO: add package path

## Other packages
### Static route manager
Since the IP routes on the nodes are essentially forming a state (in the kernel), those need to have a representation in the operator's scope and the controller loops (as state-less layers) can not own this data. This package provides ownership for the IP routes which are created by the operator. The package provides a permanent go-routine with function interfaces to manage static routes, including creating, deleting and querying them.

The package, moreover gives an event source which can be used to detect changes in the routes which are managed by the operator. The changes are detected using the netlink kernel interface, filtered for route changes. When a route add or delete request is handled, it shall be double-checked with the same event interface, before returning the request result to the sender (i.e. to return only if the route change is reported back by the kernel).

TODO: add package path

## Metrics
TODO

## Limitations
The current implementation only supports IPv4.
