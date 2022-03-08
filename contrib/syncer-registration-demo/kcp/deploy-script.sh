#!/bin/bash

# The namespace in which the kcp server will be installed
KCP_SERVER_NAMESPACE=${KCP_SERVER_NAMESPACE:-"default"}

BUILD_IMAGE=${BUILD_IMAGE:-"false"}
CONTAINER_BUILDER=${CONTAINER_BUILDER:-"docker"}
KCP_REPO=${KCP_REPO:-"https://github.com/skeeey/kcp"}
KCP_IMAGE=${KCP_IMAGE:-"quay.io/skeeey/kcp:latest"}

DEMO_ROOT="$(dirname "${BASH_SOURCE[0]}")"

cp ${DEMO_ROOT}/deploy/kustomization.yaml ${DEMO_ROOT}/deploy/kustomization.yaml.bak
sed "s/default/${KCP_SERVER_NAMESPACE}/g" ${DEMO_ROOT}/deploy/kustomization.yaml.bak > ${DEMO_ROOT}/deploy/kustomization.yaml

if [ "$1"x = "clean"x ]; then
    kubectl delete secrets kcp-admin-kubeconfig -n ${KCP_SERVER_NAMESPACE} --ignore-not-found
    kubectl delete -k ${DEMO_ROOT}/deploy
    mv ${DEMO_ROOT}/deploy/kustomization.yaml.bak ${DEMO_ROOT}/deploy/kustomization.yaml
    exit 0
fi

# build image if it is required
if [ "$BUILD_IMAGE" = "true" ]; then
    KCP_ROOT="${DEMO_ROOT}/kcp"
    rm -rf $KCP_ROOT
    git clone $KCP_REPO
    pushd $KCP_ROOT
    $CONTAINER_BUILDER build -f Dockerfile -t ${KCP_IMAGE} .
    $CONTAINER_BUILDER push ${KCP_IMAGE}
    popd
fi

#TODO allow set the kcp-acm ca
#TODO set the kcp user token

kubectl apply -k ${DEMO_ROOT}/deploy
mv ${DEMO_ROOT}/deploy/kustomization.yaml.bak ${DEMO_ROOT}/deploy/kustomization.yaml
