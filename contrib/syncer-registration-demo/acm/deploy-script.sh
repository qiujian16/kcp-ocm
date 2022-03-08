#!/bin/bash

# The namespace in which the kcp-acm integration controller will be installed
KCP_ACM_CTRL_NAMESPACE=${KCP_ACM_CTRL_NAMESPACE:-"default"}

BUILD_IMAGE=${BUILD_IMAGE:-"false"}

DEMO_ROOT="$(dirname "${BASH_SOURCE[0]}")"

cp ${DEMO_ROOT}/deploy/kustomization.yaml ${DEMO_ROOT}/deploy/kustomization.yaml.bak
sed "s/default/${KCP_ACM_CTRL_NAMESPACE}/g" ${DEMO_ROOT}/deploy/kustomization.yaml.bak > ${DEMO_ROOT}/deploy/kustomization.yaml

if [ "$1"x = "clean"x ]; then
    rm -f rootca.crt rootca.key
    kubectl delete secret kcp-acm-integration-ca -n ${KCP_ACM_CTRL_NAMESPACE} --ignore-not-found
    kubectl delete -k ${DEMO_ROOT}/deploy
    mv ${DEMO_ROOT}/deploy/kustomization.yaml.bak ${DEMO_ROOT}/deploy/kustomization.yaml
    exit 0
fi

# build image if it is required
if [ "$BUILD_IMAGE" = "true" ]; then
    ROOT_DIR="$(cd ${DEMO_ROOT}/../../.. && pwd)"

    pushd $ROOT_DIR
    make images
    popd
fi

kubectl get namespace ${KCP_ACM_CTRL_NAMESPACE} &> /dev/null || kubectl create namespace ${KCP_ACM_CTRL_NAMESPACE}

openssl genrsa -out ${DEMO_ROOT}/rootca.key 2048
openssl req -x509 -new -nodes -key ${DEMO_ROOT}/rootca.key -sha256 -days 1024 -subj "/C=CN/ST=AA/L=AA/O=OCM/CN=OCM" -out ${DEMO_ROOT}/rootca.crt

kubectl delete secret kcp-acm-integration-ca -n ${KCP_ACM_CTRL_NAMESPACE} --ignore-not-found
kubectl create secret generic kcp-acm-integration-ca -n ${KCP_ACM_CTRL_NAMESPACE} --from-file=${DEMO_ROOT}/rootca.crt --from-file=${DEMO_ROOT}/rootca.key

#TODO allow set the kcp server admin config

kubectl apply -k ${DEMO_ROOT}/deploy

mv ${DEMO_ROOT}/deploy/kustomization.yaml.bak ${DEMO_ROOT}/deploy/kustomization.yaml
