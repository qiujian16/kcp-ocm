#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
ROOT_DIR="$( cd ${CURRENT_DIR}/../.. && pwd)"

BUILD_BINARY=${BUILD_BINARY:-"true"}
IN_CLUSTER=${IN_CLUSTER:-"false"}
ENABLE_CLIENT_CA=${ENABLE_CLIENT_CA:-"false"}

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

    export HUB_KUBECONFIG=${DEMO_DIR}/kubeconfig/hub.kubeconfig
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
    export KCP_KUBECONFIG="${DEMO_DIR}"/.kcp/root.kubeconfig
    echo "Waiting for KCP server to be started..."
    wait_command "test -f ${DEMO_DIR}/kcp-started"

fi

CTRL_ARGS="--disable-leader-election --namespace=default --kcp-kubeconfig=${KCP_KUBECONFIG} --kubeconfig=${HUB_KUBECONFIG}"

if [ "$ENABLE_CLIENT_CA" = "true" ]; then
    if [ -z "$CLIENT_CA_FILE" ]; then
        # the client ca file is not defined, use our generated ca and key
        export CLIENT_CA_FILE="${DEMO_DIR}"/rootca.crt
        export CLIENT_CA_KEY_FILE="${DEMO_DIR}"/rootca.key
        CTRL_ARGS="${CTRL_ARGS} --kcp-ca=${CLIENT_CA_FILE} --kcp-key=${CLIENT_CA_KEY_FILE}"
    fi
fi

if [ "$IN_CLUSTER" = "true" ]; then
    echo "Deploy the controller in the hub cluster with HUB_KUBECONFIG=${HUB_KUBECONFIG}, KCP_KUBECONFIG=${KCP_KUBECONFIG}"
    if [ "$ENABLE_CLIENT_CA" = "true" ]; then
        pushd $ROOT_DIR
        make deploy-with-client-ca
        popd
        exit 0
    fi
    
    pushd $ROOT_DIR
    make deploy
    popd
    exit 0
fi

(cd "${ROOT_DIR}" && exec "${ROOT_DIR}"/kcp-ocm manager ${CTRL_ARGS}) &> kcp-ocm.log &
KCP_OCM_PID=$!
echo "KCP and OCM integration controller started: ${KCP_OCM_PID}. Press <ctrl>-C to terminate."
wait
