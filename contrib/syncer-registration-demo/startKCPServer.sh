#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

BUILD_BINARY=${BUILD_BINARY:-"false"}
KCP_DIR="${DEMO_DIR}"/kcp

source "${DEMO_DIR}"/utils

rm -rf ${DEMO_DIR}/.kcp

# build binary if it is required
if [ "$BUILD_BINARY" = "true" ]; then
    echo "Building kcp ..."
    rm -rf kcp
    git clone --depth 1 https://github.com/skeeey/kcp.git
    pushd $KCP_DIR
    make build
    if [ ! -f "bin/kcp" ]; then
        echo "kcp does not exist. Compilation probably failed"
        exit 1
    fi
    popd
fi

# TODO make this optional
generate_ca "${DEMO_DIR}"

echo "Starting KCP server ..."
(cd "${DEMO_DIR}" && exec "${KCP_DIR}"/bin/kcp start --client-ca-file "${DEMO_DIR}"/rootca.crt) &> kcp.log &
KCP_PID=$!
echo "KCP server started: $KCP_PID"

echo "Waiting for KCP server to be ready..."
wait_command "grep 'Serving securely' ${DEMO_DIR}/kcp.log"
wait_command "grep 'Ready to start controllers' ${DEMO_DIR}/kcp.log"

touch "${DEMO_DIR}/kcp-started"

echo "Prepare a workspace shard for current KCP server"
export KUBECONFIG=${DEMO_DIR}/.kcp/admin.kubeconfig
kubectl create namespace default 
kubectl create secret generic kubeconfig --from-file=kubeconfig=${KUBECONFIG}
kubectl apply -f "${DEMO_DIR}"/workspace/workspaceshard.crd.yaml
kubectl apply -f "${DEMO_DIR}"/workspace/workspaceshard.yaml
unset KUBECONFIG

echo "KCP server is ready. Press <ctrl>-C to terminate."
wait
