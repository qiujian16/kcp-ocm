#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
KCP_DIR="${DEMO_DIR}"/kcp

BUILD_BINARY=${BUILD_BINARY:-"true"}
ENABLE_CLIENT_CA=${ENABLE_CLIENT_CA:-"false"}
ENABLE_USER_TOKEN=${ENABLE_USER_TOKEN:-"false"}

KCP_SERVER_ARGS=""

source "${DEMO_DIR}"/utils

rm -rf ${DEMO_DIR}/.kcp

# build binary if it is required
if [ "$BUILD_BINARY" = "true" ]; then
    echo "Building kcp ..."
    rm -rf kcp
    git clone --branch release-0.4 --depth 1 https://github.com/kcp-dev/kcp.git
    pushd $KCP_DIR
    make build
    if [ ! -f "bin/kcp" ]; then
        echo "kcp does not exist. Compilation probably failed"
        exit 1
    fi
    popd
fi

if [ "$ENABLE_CLIENT_CA" = "true" ]; then
    if [ -z "$CLIENT_CA_FILE" ]; then
        # the client ca file is not defined, generate ca by ourselves
        generate_ca "${DEMO_DIR}"
        CLIENT_CA_FILE="${DEMO_DIR}"/rootca.crt
        KCP_SERVER_ARGS="${KCP_SERVER_ARGS} --client-ca-file ${CLIENT_CA_FILE}"
    fi
fi

if [ "$ENABLE_USER_TOKEN" = "true" ]; then
    echo "kcp-user-token,kcp-user,1111-1111-1111-1111,\"kcp-team\"" > kcp.tokens
    KCP_SERVER_ARGS="${KCP_SERVER_ARGS} --token-auth-file "${DEMO_DIR}"/kcp.tokens"
fi

echo "Starting KCP server ..."
(cd "${DEMO_DIR}" && exec "${KCP_DIR}"/bin/kcp start $KCP_SERVER_ARGS) &> kcp.log &
KCP_PID=$!
echo "KCP server started: $KCP_PID"

echo "Waiting for KCP server to be ready..."
wait_command "grep 'Serving securely' ${DEMO_DIR}/kcp.log"
wait_command "grep 'Ready to start controllers' ${DEMO_DIR}/kcp.log"

touch "${DEMO_DIR}/kcp-started"

kubectl config view --kubeconfig "${DEMO_DIR}"/.kcp/admin.kubeconfig --minify --flatten --context=root > "${DEMO_DIR}"/.kcp/root.kubeconfig
kubectl config view --kubeconfig "${DEMO_DIR}"/.kcp/admin.kubeconfig --minify --flatten --context=root > "${DEMO_DIR}"/.kcp/demo.kubeconfig

echo "KCP server is ready. Press <ctrl>-C to terminate."
wait
