# Terraformer

Terraformer is a tool that can execute Terraform commands (`apply`, `destroy` and `validate`) and can be run as a Pod
inside a Kubernetes cluster.
The Terraform configuration and state files (`main.tf`, `variables.tf`, `terraform.tfvars` and `terraform.tfstate`)
are stored as ConfigMaps and Secrets in the Kubernetes cluster and will be retrieved and updated by Terraformer.

For talking to the API server, Terraformer will either use the service account of its Pod or the provided kubeconfig
(via the `--kubeconfig` flag). Names of the ConfigMaps and Secrets have to be provided via command line flags.
The namespace of the objects can be specified via `--namespace` or will be defaulted to the Pod's namespace.

For more details and example Kubernetes manifests see the [example](example) directory.

**:warning: Running Terraformer as a Job**

Usually, `terraformer apply|destroy|validate` runs within a single Pod. Please note, that running Terraformer as a Job
is not recommended. The Job object will start a new Pod if the first Pod fails or is deleted (for example due to a Node
hardware failure or a reboot). Thus, you may end up in a situation with two running Terraformer Pods at the same time
which can fail with conflicts.

## State file watcher + update worker

While Terraform itself is running, Terraformer watches the state file for changes and updates the state ConfigMap as
soon as a change occurs. Internally, this is achieved by leveraging standard Kubernetes controller mechanisms: the
file watcher inserts an update key into a queue for each file write event, and a separate worker goroutine reads from
the queue and updates the state ConfigMap for every key.

After Terraform exits, Terraformer tries to update the state ConfigMap one last time, and retries the operation with
the exponential backoff until it succeeds or times out.

## Signal handling

Apart from dealing with Terraform configuration and state, Terraformer also handles Pod lifecycle event, i.e. shutdown
signals. It will try to relay `SIGINT` and `SIGTERM` to the running Terraform process in order to stop the ongoing
infrastructure operations on Pod deletion.

## How to run it locally

The `Makefile` specifies targets for running and developing Terraformer locally:

### `make run`

`make run` (with an optional variable `COMMAND=[command]`) will run the given terraformer command (or `apply` by
default) locally on your machine using `go run`. You will have to point Terraformer to a running cluster, where the
config and state is stored, via the `KUBECONFIG` environment variable. This can be any Kubernetes cluster, either a
local one (setup using minikube, kind or the [local nodeless garden](https://github.com/gardener/gardener/blob/master/docs/development/local_setup.md#start-the-gardener))
or even a remote one.

```
$ make run COMMAND=apply
# running `go run ./cmd/terraformer apply`
...
```

### `make start`

`make start` is similar to `make run`, but instead of directly running terraformer on your machine it starts a docker
development container with all the prerequisites installed (like go and terraform) and starts terraform via `go run` in
it. This allows running terraformer in an environment that is very close to the environment that it is executed in, when
running it in a Kubernetes Pod.

Just as with `make run`, you will have to set your `KUBECONFIG` to point to a Kubernetes cluster used for hosting config
and state. If you are using kind, you might want to use the [example cluster config](example/00-kind-cluster.yaml) to
create a cluster with certificates, that will be trusted from inside the docker VM (on Mac or Windows).

When running this for the first time, the development container will have to be built, which might a bit. But after that
the image will be reused and only rebuilt if you change something in the files relevant for the image build.

### `make start-dev-container`

The mentioned development container can also be started directly by executing running `make start-dev-container`.
This will open a shell in this container, where you can execute all commands for development and testing like
`make`, `go`, `ginkgo` and so on.

## How to test it

Terraformer currently comes with two sets of tests: a bunch of unit tests and a binary e2e test suite.
Both of these can be executed by running `make test`.
Most tests are executed against a local control plane by leveraging the [envtest package](https://github.com/kubernetes-sigs/controller-runtime/tree/master/pkg/envtest).
Therefore, the `kube-apiserver` and `etcd` binaries are fetched to `bin/kubebuilder` and will be started by the tests.

You can also run the tests against a different cluster for debugging or other purposes by setting the 
`USE_EXISTING_CLUSTER` and `KUBECONFIG` environment variable:

```
KUBECONFIG=$HOME/.kube/configs/local/garden.yaml USE_EXISTING_CLUSTER=true make test
```

## Docker Images

Terraformer images are built with every pipeline run and pushed to gcr.io.
Alternatively, they can be built manually using `make docker-image`.
The list of existing images and tags can be found in [eu.gcr.io/gardener-project/gardener/terraformer](https://eu.gcr.io/gardener-project/gardener/terraformer).

In the build container and apart from `terraform` and other dependencies, the `terraform-bundle` binary is installed.
It is used to install all Terraform provider plugins specified in [`terraform-bundle.hcl`](terraform-bundle.hcl).

Currently, the terraformer docker image contains all provider plugins, but we might split it up into multiple docker
images with only one provider plugin in the future (see https://github.com/gardener/terraformer/issues/46).
