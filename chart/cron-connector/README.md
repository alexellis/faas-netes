# OpenFaaS Cron Connector

## Pre-requisite

- Install OpenFaaS
You must have a working OpenFaaS installation. You can find [instructions in the docs](https://docs.openfaas.com/deployment/kubernetes/#pick-helm-or-yaml-files-for-deployment-a-or-b), including instructions to also install OpenFaaS via Helm.

## Install the Chart

- Add OpenFaaS chart repo to helm and install using the following command.

```bash
# Add OpenFaaS chart repo
$ helm repo add openfaas https://openfaas.github.io/faas-netes/

$ helm upgrade cron-connector openfaas/cron-connector \
    --install \
    --namespace openfaas
```

## Deploying using local repository

```bash
git clone https://github.com/openfaas/faas-netes.git

cd faas-netes

helm upgrade --namespace openfaas \
  --install cron-connector \
  ./chart/cron-connector
```

## Configuration

cron-connector options in values.yaml

| Parameter                 | Description                                      | Default                                  |
|---------------------------|--------------------------------------------------|------------------------------------------|
| `image`                   | The cron-connector image that should be deployed | `ghcr.io/openfaas/cron-connector:0.3.2`  |
| `gateway_url`             | The URL for the API gateway.                     | `"http://gateway.openfaas:8080"`         |
| `basic_auth`              | Enable or disable basic auth                     | `true`                                   |
| `asynchronous_invocation` | Use asynchronous function endpoint               | `false`                                  |
## Removing the cron-connector

All components can be cleaned up with helm:

```bash
helm delete --purge cron-connector
```
