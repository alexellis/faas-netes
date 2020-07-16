# OpenFaaS Ingress Operator

The [Ingress operator](https://github.com/openfaas-incubator/ingress-operator) gets custom domains and TLS for your OpenFaaS Functions through the FunctionIngress CRD
## Prerequisites

- Install OpenFaaS

  You must have a working OpenFaaS installation. You can find [instructions in the docs](https://docs.openfaas.com/deployment/kubernetes/#pick-helm-or-yaml-files-for-deployment-a-or-b), including instructions to also install OpenFaaS via Helm.


## Install the Chart

- Add the OpenFaaS chart repo and deploy the `ingress-operator` chart. We recommend installing it in the same namespace as the rest of OpenFaaS

```sh
$ helm repo add openfaas https://openfaas.github.io/faas-netes/
$ helm upgrade ingress-operator openfaas/ingress-operator \
    --install \
    --namespace openfaas
```

> The above command will also update your helm repo to pull in any new releases.

## Configuration

Additional ingress-operator options in `values.yaml`.

| Parameter                | Description                                                                            | Default                        |
| ------------------------ | -------------------------------------------------------------------------------------- | ------------------------------ |
| `create` | Create the ingress-operator component | `false` |
| `replicas` | Replicas of the ingress-operator| `1` |
| `image` | Container image used in ingress-operator| `openfaas/ingress-operator:0.6.2` |
| `resources` | Limits and requests for memory and CPU usage | Memory Requests: 25Mi |
| `imagePullPolicy` | Image Pull Policy | `Always` |
| `functionNamespace` | Namespace for functions | `openfaas-fn` |

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`.
See values.yaml for detailed configuration.

## Removing the ingress-operator

All control plane components can be cleaned up with helm:

For Helm 2

```sh
$ helm delete --purge ingress-operator
```

For Helm 3

```sh
$ helm uninstall ingress-operator
```