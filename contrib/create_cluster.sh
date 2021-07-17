#!/usr/bin/env bash

DEVENV=${OF_DEV_ENV:-kind}
KUBE_VERSION=v1.18.19

EXISTS=$(kind get clusters | grep "$DEVENV")

if [ "$EXISTS" = "$DEVENV" ]; then
    while true; do
    read -p "Kind cluster '$DEVENV' already exists, do you want to continue with this cluster? (yes/no) " yn
    case $yn in
        [Yy]* ) break;;
        [Nn]* ) exit 1;;
        * ) echo "Please answer yes or no.";;
    esac
done
fi


if [ "$EXISTS" != "$DEVENV" ]; then
    echo ">>> Creating Kubernetes ${KUBE_VERSION} cluster ${DEVENV}"

    kind create cluster --wait 5m --image kindest/node:${KUBE_VERSION} --name "$DEVENV" -v 1
fi

echo ">>> Waiting for CoreDNS"
kubectl --context "kind-$DEVENV" -n kube-system rollout status deployment/coredns
