#!/bin/bash

# The namespace in which the kcp server will be installed
if [ ! -n "$KCP_ACM_INTEGRATION_NAMESPACE" ]; then
    echo "The kcp server installation namespace is not defined, please set it by export KCP_ACM_INTEGRATION_NAMESPACE=<namespace>."
    exit 1
fi

if [ ! -n "$KCPIP" ]; then
    echo "The KCP external accessible IP is required. Please set it by export KCPIP=<kcp_external_accessible_ip>."
    exit 1
fi

BUILD_IMAGE=${BUILD_IMAGE:-"false"}
CONTAINER_BUILDER=${CONTAINER_BUILDER:-"docker"}
KCP_REPO=${KCP_REPO:-"https://github.com/skeeey/kcp"}
KCP_IMAGE=${KCP_IMAGE:-"quay.io/skeeey/kcp:latest"}

DEMO_DIR="$(dirname "${BASH_SOURCE[0]}")"

cp ${DEMO_DIR}/deploy/kustomization.yaml ${DEMO_DIR}/deploy/kustomization.yaml.bak
sed "s/default/${KCP_ACM_INTEGRATION_NAMESPACE}/g" ${DEMO_DIR}/deploy/kustomization.yaml.bak > ${DEMO_DIR}/deploy/kustomization.yaml

if [ "$1"x = "clean"x ]; then
    kubectl delete secrets kcp-admin-kubeconfig -n ${KCP_ACM_INTEGRATION_NAMESPACE} --ignore-not-found
    kubectl delete -k ${DEMO_DIR}/deploy
    mv ${DEMO_DIR}/deploy/kustomization.yaml.bak ${DEMO_DIR}/deploy/kustomization.yaml
    exit 0
fi

# build image if it is required
if [ "$BUILD_IMAGE" = "true" ]; then
    KCP_ROOT="${DEMO_DIR}/kcp"
    rm -rf $KCP_ROOT
    git clone $KCP_REPO
    pushd $KCP_ROOT
    $CONTAINER_BUILDER build -f Dockerfile -t ${KCP_IMAGE} .
    $CONTAINER_BUILDER push ${KCP_IMAGE}
    popd
fi

kubectl get namespace ${KCP_ACM_INTEGRATION_NAMESPACE} &> /dev/null || kubectl create namespace ${KCP_ACM_INTEGRATION_NAMESPACE}

#kubectl apply -k ${DEMO_DIR}/deploy
kubectl kustomize ${DEMO_DIR}/deploy | sed "s/127.0.0.1/${KCPIP}/g" | kubectl apply -f -
mv ${DEMO_DIR}/deploy/kustomization.yaml.bak ${DEMO_DIR}/deploy/kustomization.yaml
