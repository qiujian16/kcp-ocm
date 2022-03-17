#!/usr/bin/env bash

# The namespace in which the kcp-acm integration controller will be installed
if [ ! -n "$KCP_ACM_INTEGRATION_NAMESPACE" ]; then
    echo "The controller installation namespace is not defined, please set it by export KCP_ACM_INTEGRATION_NAMESPACE=<namespace>."
    exit 1
fi

BUILD_IMAGE=${BUILD_IMAGE:-"false"}

DEMO_DIR="$(dirname "${BASH_SOURCE[0]}")"

cp ${DEMO_DIR}/deploy/kustomization.yaml ${DEMO_DIR}/deploy/kustomization.yaml.bak
sed "s/default/${KCP_ACM_INTEGRATION_NAMESPACE}/g" ${DEMO_DIR}/deploy/kustomization.yaml.bak > ${DEMO_DIR}/deploy/kustomization.yaml

if [ "$1"x = "clean"x ]; then
    rm -f rootca.crt rootca.key
    kubectl delete secret kcp-acm-integration-ca -n ${KCP_ACM_INTEGRATION_NAMESPACE} --ignore-not-found
    kubectl delete -k ${DEMO_DIR}/deploy
    mv ${DEMO_DIR}/deploy/kustomization.yaml.bak ${DEMO_DIR}/deploy/kustomization.yaml
    exit 0
fi

# build image if it is required
if [ "$BUILD_IMAGE" = "true" ]; then
    REPO_DIR="$(cd ${DEMO_DIR}/../../.. && pwd)"
    pushd $REPO_DIR
    make images
    popd
fi

kubectl get namespace ${KCP_ACM_INTEGRATION_NAMESPACE} &> /dev/null || kubectl create namespace ${KCP_ACM_INTEGRATION_NAMESPACE}

openssl genrsa -out ${DEMO_DIR}/rootca.key 2048
openssl req -x509 -new -nodes -key ${DEMO_DIR}/rootca.key -sha256 -days 1024 -subj "/C=CN/ST=AA/L=AA/O=OCM/CN=OCM" -out ${DEMO_DIR}/rootca.crt

kubectl delete secret kcp-acm-integration-ca -n ${KCP_ACM_INTEGRATION_NAMESPACE} --ignore-not-found
kubectl create secret generic kcp-acm-integration-ca -n ${KCP_ACM_INTEGRATION_NAMESPACE} --from-file=${DEMO_DIR}/rootca.crt --from-file=${DEMO_DIR}/rootca.key

kubectl apply -k ${DEMO_DIR}/deploy

mv ${DEMO_DIR}/deploy/kustomization.yaml.bak ${DEMO_DIR}/deploy/kustomization.yaml
