#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
ROOT_DIR="$( cd ${CURRENT_DIR}/../.. && pwd)"

BUILD_BINARY=${BUILD_BINARY:-"false"}

source "${DEMO_DIR}"/utils

# build binary if it is required
if [ "$BUILD_BINARY" = "true" ]; then
    echo "Building kcp-ocm controller ..."
    pushd $ROOT_DIR
    rm -f kcp-ocm
    make build
    if [ ! -f "kcp-ocm" ]; then
        echo "kcp-ocm does not exist.Compilation probably failed"
        exit 1
    fi
    popd
fi

# HUB_KUBECONFIG is not defined, run controller in the local demo env
if [ -z "$HUB_KUBECONFIG" ]; then
    rm -rf "${DEMO_DIR}"/kubeconfig
    mkdir -p "${DEMO_DIR}"/kubeconfig

    kubectl config view --minify --flatten --context=kind-hub > "${DEMO_DIR}"/kubeconfig/hub.kubeconfig
    kubectl config view --minify --flatten --context=kind-cluster1 > "${DEMO_DIR}"/kubeconfig/cluster1.kubeconfig
    kubectl config view --minify --flatten --context=kind-cluster2 > "${DEMO_DIR}"/kubeconfig/cluster2.kubeconfig

    HUB_KUBECONFIG=${DEMO_DIR}/kubeconfig/hub.kubeconfig
    KUBECTL="kubectl --kubeconfig=${HUB_KUBECONFIG}"

    $KUBECTL get managedclusters 
    if [[ "$?" != 0 ]]; then
        echo "Failed to get managed clusters with ${KUBECONFIG}."
        exit 1
    fi

    echo "Prepare a clusterset"
    $KUBECTL apply -f "${DEMO_DIR}"/clusterset/clusterset.yaml
    $KUBECTL label managedclusters cluster1 cluster.open-cluster-management.io/clusterset=dev --overwrite
    $KUBECTL label managedclusters cluster2 cluster.open-cluster-management.io/clusterset=dev --overwrite
fi

# KCP_KUBECONFIG is not defined, wait kcp in the local demo env
if [ -z "$KCP_KUBECONFIG" ]; then
    KCP_KUBECONFIG="${DEMO_DIR}"/.kcp/admin.kubeconfig
    echo "Waiting for KCP server to be started..."
    wait_command "test -f ${DEMO_DIR}/kcp-started"
fi

${ROOT_DIR}/kcp-ocm manager \
    --kcp-kubeconfig="${KCP_KUBECONFIG}" \
    --kubeconfig="${HUB_KUBECONFIG}" \
    --kcp-ca="${DEMO_DIR}/rootca.crt" \
    --kcp-key="${DEMO_DIR}/rootca.key" \
    --namespace=default
