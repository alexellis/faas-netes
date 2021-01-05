# OpenFaaS - Serverless Functions Made Simple

<img src="https://blog.alexellis.io/content/images/2017/08/faas_side.png" alt="OpenFaaS logo" width="60%">

[OpenFaaS](https://github.com/openfaas/faas) (Functions as a Service) is a framework for building serverless functions with Docker and Kubernetes which has first class support for metrics. Any process can be packaged as a function enabling you to consume a range of web events without repetitive boiler-plate coding.

## Highlights

* Ease of use through UI portal and *one-click* install
* Write functions in any language for Linux or Windows and package in Docker/OCI image format
* Portable - runs on existing hardware or public/private cloud. Native [Kubernetes](https://github.com/openfaas/faas-netes) support, Docker Swarm also available
* [Operator / CRD option available](https://github.com/openfaas/faas-netes/)
* [faas-cli](http://github.com/openfaas/faas-cli) available with stack.yml for creating and managing functions
* Auto-scales according to metrics from Prometheus
* Scales to zero and back again and can be tuned at a per-function level
* Works with service-meshes
    * Tested with [Istio](https://istio.io) including mTLS
    * Tested with [Linkerd2](https://github.com/openfaas-incubator/openfaas-linkerd2) including mTLS and traffic splitting with SMI

## Deploy OpenFaaS

### 1) Install with arkade

It is recommended that you use arkade to install OpenFaaS. arkade is a CLI tool which automates the helm CLI and chart download and installation. The `openfaas` app also has a number of options available via `arkade install openfaas --help`

The installation with arkade is as simple as the following which installs OpenFaaS, sets up an Ingress record, and a TLS certificate with cert-manager.

```bash
arkade install openfaas
arkade install openfaas-ingress \
  --domain openfaas.example.com \
  --email wm@example.com
```

See a complete example here: [Get TLS for OpenFaaS the easy way with arkade](https://blog.alexellis.io/tls-the-easy-way-with-openfaas-and-k3sup/)

If you wish to continue without using arkade, read on for instructions.

### 2) Install with helm

These instructions are for Intel (normal computers), jump to the end of the document for ARM and Raspberry Pi.

To use the chart, you will need Helm, we recommend helm 3:

Get it from arkade:

```bash
arkade get helm
```

Or use the helm3 installer:

```bash
curl -sSLf https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash
```

We recommend creating two namespaces, one for the OpenFaaS *core services* and one for the *functions*:

```sh
kubectl apply -f https://raw.githubusercontent.com/openfaas/faas-netes/master/namespaces.yml
```

You will now have `openfaas` and `openfaas-fn`. If you want to change the names or to install into multiple installations then edit `namespaces.yml` from the `faas-netes` repo.

Add the OpenFaaS `helm` chart:

```sh
helm repo add openfaas https://openfaas.github.io/faas-netes/
```

Now decide how you want to expose the services and edit the `helm upgrade` command as required.

* To use NodePorts (default) pass no additional flags
* To use a LoadBalancer add `--set serviceType=LoadBalancer`
* To use an IngressController add `--set ingress.enabled=true`

> Note: even without a LoadBalancer or IngressController you can access your gateway at any time via `kubectl port-forward`.

### Deploy

Note that the commands will differ slightly between versions, if not specified, the instructions are for helm 2.

Now deploy OpenFaaS from the helm chart repo:

```sh
helm repo update \
 && helm upgrade openfaas --install openfaas/openfaas \
    --namespace openfaas  \
    --set functionNamespace=openfaas-fn \
    --set generateBasicAuth=true
```

> The above command will also update your helm repo to pull in any new releases.

Retrieve the OpenFaaS credentials with:

```sh
PASSWORD=$(kubectl -n openfaas get secret basic-auth -o jsonpath="{.data.basic-auth-password}" | base64 --decode) && \
echo "OpenFaaS admin password: $PASSWORD"
```

#### Generate basic-auth credentials

The chart has a pre-install hook which can generate basic-auth credentials, enable it with `--set generateBasicAuth=true`.

Alternatively, you can set `generateBasicAuth` to `false` and generate or supply the basic-auth credentials yourself. This is the option you may want if you are using `helm template`.

```sh
# generate a random password
PASSWORD=$(head -c 12 /dev/urandom | shasum| cut -d' ' -f1)
kubectl -n openfaas create secret generic basic-auth \
--from-literal=basic-auth-user=admin \
--from-literal=basic-auth-password="$PASSWORD"

echo "OpenFaaS admin password: $PASSWORD"
```

#### Tuning cold-start

The concept of a cold-start in OpenFaaS only applies if you A) use faas-idler and B) set a specific function to scale to zero. Otherwise there is not a cold-start, because at least one replica of your function remains available.

There are two ways to reduce the Kubernetes cold-start for a pre-pulled image, which is around 1-2 seconds.

1) Don't set the function to scale down to zero, just set it a minimum availability i.e. 1/1 replicas
2) Use async invocations via the `/async-function/<name>` route on the gateway, so that the latency is hidden from the caller
3) Tune the readinessProbes to be aggressively low values. This will reduce the cold-start at the cost of more `kubelet` CPU usage

To achieve around 1s coldstart, set `values.yaml`:

```yaml
faasnetes:

# redacted
  readinessProbe:
    initialDelaySeconds: 0
    timeoutSeconds: 1
    periodSeconds: 1
  livenessProbe:
    initialDelaySeconds: 0
    timeoutSeconds: 1
    periodSeconds: 1
# redacted
  imagePullPolicy: "IfNotPresent"    # Image pull policy for deployed functions
```


In addition:

* Pre-pull images on each node
* Use an in-cluster registry to reduce the pull latency for images
* Set the `imagePullPolicy` to `IfNotPresent` so that the `kubelet` only pulls images which are not already available
* Explore alternatives such as not scaling to absolute zero, and using async calls which do not show the cold start

#### httpProbe vs. execProbe

A note on health-checking probes for functions:

* httpProbe - (`default`) most efficient. (compatible with Istio >= 1.1.5)
* execProbe - least efficient option, but compatible with Istio < 1.1.5

Use `--set faasnetes.httpProbe=true/false` to toggle between http / exec probes.

### Verify the installation

Once all the services are up and running, log into your gateway using the OpenFaaS CLI. This will cache your credentials into your `~/.openfaas/config.yml` file.

Fetch your public IP or NodePort via `kubectl get svc -n openfaas gateway-external -o wide` and set it as an environmental variable as below:

```sh
export OPENFAAS_URL=http://127.0.0.1:31112
```

If using a remote cluster, you can port-forward the gateway to your local machine:

```sh
export OPENFAAS_URL=http://127.0.0.1:8080
kubectl port-forward -n openfaas svc/gateway 8080:8080 &
```

Now log in with the CLI and check connectivity:

```sh
echo -n $PASSWORD | faas-cli login -g $OPENFAAS_URL -u admin --password-stdin

faas-cli version
```

## OpenFaaS Operator and Function CRD

If you would like to work with Function CRDs there is an alternative controller to faas-netes named [OpenFaaS Operator](https://github.com/openfaas-incubator/openfaas-operator) which can be swapped in at deployment time.
The OpenFaaS Operator is suitable for development and testing and may replace the faas-netes controller in the future.
The Operator is compatible with Kubernetes 1.9 or later.

To use it, add the flag: `--set operator.create=true` when installing with Helm.

### faas-netes vs OpenFaaS Operator

The faas-netes controller is the most tested, stable and supported version of the OpenFaaS integration with Kubernetes. In contrast the OpenFaaS Operator is based upon the codebase and features from `faas-netes`, but offers a tighter integration with Kubernetes through [CustomResourceDefinitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/). This means you can type in `kubectl get functions` for instance.

See also: [Introducing the OpenFaaS Operator](https://www.openfaas.com/blog/kubernetes-operator-crd/)

## Deployment with `helm template`

This option is good for those that have issues with or concerns about installing Tiller, the server/cluster component of helm. Using the `helm` CLI, we can pre-render and then apply the templates using `kubectl`.

1. Clone the faas-netes repository
    ```sh
    git clone https://github.com/openfaas/faas-netes.git
    cd faas-netes
    ```

2. Render the chart to a Kubernetes manifest called `openfaas.yaml`

    Helm 3:
    ```sh
    helm template \
      openfaas chart/openfaas/ \
      --namespace openfaas \
      --set basic_auth=true \
      --set functionNamespace=openfaas-fn > openfaas.yaml
    ```

    Helm 2:

    ```sh
    helm template chart/openfaas \
        --name openfaas \
        --namespace openfaas  \
        --set basic_auth=true \
        --set functionNamespace=openfaas-fn > openfaas.yaml
    ```

    You can set the values and overrides just as you would in the install/upgrade commands above.

3. Install the components using `kubectl`

    ```sh
    kubectl apply -f namespaces.yml,openfaas.yaml
    ```

Now [verify your installation](#verify-the-installation).

## Test a local helm chart

You can run the following command from within the `faas-netes/chart` folder in the `faas-netes` repo.

```sh
helm upgrade openfaas --install chart/openfaas \
    --namespace openfaas  \
    --set basic_auth=true \
    --set functionNamespace=openfaas-fn
```

## Exposing services

### NodePorts

By default a NodePort will be created for the API Gateway.

### Metrics

You temporarily access the Prometheus metrics by using `port-forward`

```sh
kubectl --namespace openfaas port-forward deployment/prometheus 31119:9090
```

Then open `http://localhost:31119` to directly query the OpenFaaS metrics scraped by Prometheus.

### LB

If you're running on a cloud such as AKS or GKE you will need to pass an additional flag of `--set serviceType=LoadBalancer` to tell `helm` to create LoadBalancer objects instead. An alternative to using multiple LoadBalancers is to install an Ingress controller.

### Deploy with an IngressController

In order to make use of automatic ingress settings you will need an IngressController in your cluster such as Traefik or Nginx.

Add `--set ingress.enabled` to enable ingress pass `--set ingress.enabled=true` when running the installation via `helm`.

By default services will be exposed with following hostnames (can be changed, see values.yaml for details):

* `gateway.openfaas.local`

### Endpoint load-balancing

Some configurations in combination with client-side KeepAlive settings may because load to be spread unevenly between replicas of a function. If you experience this, there are three ways to work around it:

* [Install Linkerd2](https://github.com/openfaas-incubator/openfaas-linkerd2) which takes over load-balancing from the Kubernetes L4 Service (recommended)
* Disable KeepAlive in the client-side code (not recommended)
* Configure the gateway to pass invocations through to the faas-netes provider (alternative to using Linkerd2)

    ```sh
    --set gateway.directFunctions=false
    ```

    In this mode, all invocations will pass through the gateway to faas-netes, which will look up endpoint IPs directly from Kubernetes, the additional hop may add some latency, but will do fair load-balancing, even with KeepAlive.

### SSL / TLS

If you require TLS/SSL then please make use of an IngressController. A full guide is provided to [enable TLS for the OpenFaaS Gateway using cert-manager and Let's Encrypt](https://docs.openfaas.com/reference/ssl/kubernetes-with-cert-manager/).

### Service meshes
If you use a service mesh like Linkerd or Istio in your cluster, then you should enable the `directFunctions` mode using:

```sh
--set gateway.directFunctions=true
```

### Istio mTLS

To install OpenFaaS with Istio mTLS pass  `--set istio.mtls=true` and disable the HTTP probes:

```sh
helm upgrade openfaas --install chart/openfaas \
    --namespace openfaas  \
    --set basic_auth=true \
    --set functionNamespace=openfaas-fn \
    --set exposeServices=false \
    --set faasnetes.httpProbe=false \
    --set httpProbe=false \
    --set gateway.directFunctions=true \
    --set istio.mtls=true
```

The above command will enable mTLS for the openfaas control plane services and functions excluding NATS.

> Note that the above instructions were tested on GKE 1.13 and Istio 1.2

## Zero scale

### Scale-up from zero (on by default)

Scaling up from zero replicas is enabled by default, to turn it off set `scaleFromZero` to `false` in the helm chart options for the `gateway` component.

```sh
--set gateway.scaleFromZero=true/false
```

### Scale-down to zero (off by default)

Scaling down to zero replicas can be achieved either through the REST API and your own controller, or by using the [faas-idler](https://github.com/openfaas-incubator/faas-idler) component.

By default the faas-idler is set to only do a dryRun and to not scale any functions down.

```sh
--set faasIdler.dryRun=true/false
```

The faas-idler will only scale down functions which have marked themselves as eligible for this behaviour through the use of a label: `com.openfaas.scale.zero=true`.

See also: [faas-idler README](https://docs.openfaas.com/architecture/autoscaling/#zero-scale).

## Removing the OpenFaaS

All control plane components can be cleaned up with helm:

Helm 3:

```sh
helm delete openfaas --namespace openfaas
```

Helm 2:

```sh
helm delete --purge openfaas
```

Follow this by the following to remove all other associated objects:

```sh
kubectl delete namespace openfaas openfaas-fn
```

In some cases your additional functions may need to be either deleted before deleting the chart with `faas-cli` or manually deleted using `kubectl delete`.

## ARM

If you would like to deploy OpenFaaS to ARM i.e. Raspberry Pi, ARM64 machines provided by Packet.net, Scaleway or to AWS Graviton, then you should use the appropriate values.yaml file.

* `values-armhf.yml` - for Raspberry Pi and other ARMv7 boards (run `uname -a` to find out which you have)
* `values-arm64.yml` - for everything else (`arm64` or `aarch64`)

It is recommended that you install OpenFaaS to ARM machines [using k3sup](https://k3sup.dev/) instead of helm directly since it will determine the correct values to be used.

See also: [Kubernetes and Raspberry Pi in the docs](https://docs.openfaas.com/deployment/kubernetes)

## Kubernetes versioning
This Helm chart currently supports version 1.16+

Note that OpenFaaS itself may support a wider range of versions, [see here](../../README.md#kubernetes-versions)

## Getting help

Feel free to seek out help using the [OpenFaaS Slack workspace](https://slack.openfaas.io/), please do not raise issues for technical support, unless you suspect and can provide instructions for reproducing an error in the chart.

## Configuration

Additional OpenFaaS options in `values.yaml`.

| Parameter               | Description                           | Default                                                    |
| ----------------------- | ----------------------------------    | ---------------------------------------------------------- |
| `functionNamespace` | Functions namespace, preferred `openfaas-fn` | `default` |
| `clusterRole` | Use a `ClusterRole` for the Operator or faas-netes. Set to `true` for multiple namespace support | `false` |
| `createCRDs` | Create the CRDs for OpenFaaS Functions and Profiles | `true` |
| `basic_auth` | Enable basic authentication on the gateway and Prometheus. Warning: do not disable. | `true` |
| `async` | Enables asynchronous function invocations. If `.nats.external.enabled` is `false`, also deploys NATS Streaming | `true` |
| `exposeServices` | Expose `NodePorts/LoadBalancer`  | `true` |
| `serviceType` | Type of external service to use `NodePort/LoadBalancer` | `NodePort` |
| `generateBasicAuth` | Generate admin password for basic authentication | `false` |
| `rbac` | Enable RBAC | `true` |
| `httpProbe` | Setting to true will use HTTP for readiness and liveness probe on the OpenFaaS system Pods (compatible with Istio >= 1.1.5) | `true` |
| `psp` | Enable [Pod Security Policy](https://kubernetes.io/docs/concepts/policy/pod-security-policy/) for OpenFaaS accounts | `false` |
| `securityContext` | Deploy with a `securityContext` set, this can be disabled for use with Istio sidecar injection | `true` |
| `openfaasImagePullPolicy` | Image pull policy for openfaas components, can change to `IfNotPresent` in offline env | `Always` |
| `openfaasImagePullSecrets` | imagePullSecrets for pulling openfaas images from private registries                  | []       |
| `kubernetesDNSDomain` | Domain name of the Kubernetes cluster | `cluster.local` |
| `operator.create` | Use the OpenFaaS operator CRD controller, default uses faas-netes as the Kubernetes controller | `false` |
| `ingress.enabled` | Create ingress resources | `false` |
| `faasnetes.httpProbe` | Use a httpProbe instead of exec | `false` |
| `ingressOperator.create` | Create the ingress-operator component | `false` |
| `ingressOperator.replicas` | Replicas of the ingress-operator| `1` |
| `ingressOperator.image` | Container image used in ingress-operator| `openfaas/ingress-operator:0.6.2` |
| `ingressOperator.resources` | Limits and requests for memory and CPU usage | Memory Requests: 25Mi |
| `faasnetes.readTimeout` | Queue worker read timeout | `60s` |
| `faasnetes.writeTimeout` | Queue worker write timeout | `60s` |
| `faasnetes.imagePullPolicy` | Image pull policy for deployed functions | `Always` |
| `faasnetes.setNonRootUser` | Force all function containers to run with user id `12000` | `false` |
| `gateway.directFunctions` | Invoke functions directly without using the provider | `true` |
| `gateway.replicas` | Replicas of the gateway, pick more than `1` for HA | `1` |
| `gateway.readTimeout` | Queue worker read timeout | `65s` |
| `gateway.writeTimeout` | Queue worker write timeout | `65s` |
| `gateway.upstreamTimeout` | Maximum duration of upstream function call, should be lower than `readTimeout`/`writeTimeout` | `60s` |
| `gateway.scaleFromZero` | Enables an intercepting proxy which will scale any function from 0 replicas to the desired amount | `true` |
| `gateway.maxIdleConns` | Set max idle connections from gateway to functions | `1024` |
| `gateway.maxIdleConnsPerHost` | Set max idle connections from gateway to functions per host | `1024` |
| `gateway.logsProviderURL` | Set a custom logs provider url | `""` |
| `queueWorker.durableQueueSubscriptions` | Whether to use a durable queue subscription | `false` |
| `queueWorker.queueGroup` | The name of the queue group used to process asynchronous function invocations | `faas` |
| `queueWorker.replicas` | Replicas of the queue-worker, pick more than `1` for HA | `1` |
| `queueWorker.ackWait` | Max duration of any async task/request | `60s` |
| `nats.channel` | The name of the NATS Streaming channel to use for asynchronous function invocations | `faas-request` |
| `nats.external.clusterName` | The name of the externally-managed NATS Streaming server | `` |
| `nats.external.enabled` | Whether to use an externally-managed NATS Streaming server | `false` |
| `nats.external.host` | The host at which the externally-managed NATS Streaming server can be reached | `""` |
| `nats.external.port` | The port at which the externally-managed NATS Streaming server can be reached | `""` |
| `nats.enableMonitoring` | Enable the NATS monitoring endpoints on port `8222` for NATS Streaming deployments managed by this chart | `false` |
| `nats.metrics.enabled` | Export Prometheus metrics for NATS, no multi-arch support  | `false` |
| `nats.metrics.image` | Container image used for the NATS Prometheus exporter, not multi-arch | `synadia/prometheus-nats-exporter:0.6.2` |
| `faasIdler.create` | Create the faasIdler component | `true` |
| `faasIdler.inactivityDuration` | Duration after which faas-idler will scale function down to 0 | `15m` |
| `faasIdler.reconcileInterval` | The time between each of reconciliation | `1m` |
| `faasIdler.dryRun` | When set to false the OpenFaaS API will be called to scale down idle functions, by default this is set to only print in the logs. | `true` |
| `prometheus.create` | Create the Prometheus component | `true` |
| `alertmanager.create` | Create the AlertManager component | `true` |
| `istio.mtls` | Create Istio policies and destination rules to enforce mTLS for OpenFaaS components and functions | `false` |


Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`.
See values.yaml for detailed configuration.
