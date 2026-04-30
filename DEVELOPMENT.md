# Development

This guide covers deploying the bpfman-operator using OLM on an OpenShift cluster.

## Prerequisites

- Access to an OpenShift cluster via `kubectl`/`oc`
- A container image registry (quay.io by default)
- `docker` or `podman` available locally

## Configuration

All image references are derived from a small set of Makefile variables that can be overridden via environment variables or on the `make` command line:

| Variable | Default | Description |
|---|---|---|
| `IMAGE_ORG` | `bpfman` | Registry namespace under `quay.io` |
| `IMAGE_TAG` | `latest` | Tag for `bpfman`, `bpfman-agent`, and `bpfman-operator` images |
| `VERSION` | contents of `VERSION` file | Operator and bundle version |
| `BUNDLE_IMG` | `quay.io/<IMAGE_ORG>/bpfman-operator-bundle:v<VERSION>` | OLM bundle image |
| `CATALOG_IMG` | `quay.io/<IMAGE_ORG>/bpfman-operator-catalog:v<VERSION>` | OLM catalog image |

For example, to use a personal registry namespace:

```sh
make bundle-deploy-openshift IMAGE_ORG=myuser IMAGE_TAG=dev
```

## Bundle Deployment

The bundle deployment builds all operator images, generates and pushes an OLM bundle, then installs the operator via `operator-sdk run bundle`.

```sh
make bundle-deploy-openshift
```

This target will:

1. Build `bpfman-operator` and `bpfman-agent` container images.
2. Push the images to the registry.
3. Generate the OLM bundle manifests.
4. Build and push the bundle image.
5. Run the bundle in the `bpfman` namespace on the current cluster.

To remove the bundle deployment:

```sh
make bundle-undeploy
```

## Catalog Deployment

The catalog deployment performs the same steps as the bundle deployment and additionally builds an OLM catalog image and applies a `CatalogSource` to the cluster. This makes the operator available through the OperatorHub UI.

```sh
make catalog-deploy
```

This target will:

1. Build `bpfman-operator` and `bpfman-agent` container images.
2. Push the images to the registry.
3. Generate the OLM bundle manifests.
4. Build and push both the bundle and catalog images.
5. Apply the `bpfmanoperator-dev-catalog` CatalogSource to the cluster.

To remove the catalog deployment:

```sh
make catalog-undeploy
```
