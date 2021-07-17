#!/usr/bin/env bash

DEVENV=${OF_DEV_ENV:-kind}
KUBE_VERSION=v1.18.19

echo ">>> Creating Kubernetes ${KUBE_VERSION} cluster ${DEVENV}"

kind create cluster --wait 5m --image kindest/node:${KUBE_VERSION} --name "$DEVENV" -v 3

echo ">>> Waiting for CoreDNS"
kubectl --context "kind-$DEVENV" -n kube-system rollout status deployment/coredns

kubectl --context "kind-$DEVENV"  -n kube-system get events
