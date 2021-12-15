#!/bin/bash

trap cleanup 1 2 3 6

cleanup() {
  echo "Killing KCP and the KCP-OCM controllers"
  kill $KCP_PID $KCP_OCM_PID
}

function comment() {
  echo -e '\033[0;33m>>> '$1' <<<\033[0m'
}

# KCP_GIT_SHA="7471fb98d0bcc28fbc5b837c9ffdbb599530f69c"

DEMO_ROOT="$( dirname "${BASH_SOURCE[0]}" )"
. ${DEMO_ROOT}/demo-magic
ROOT_DIR="$( cd ${DEMO_ROOT}/../.. && pwd)"
KUBECONFIG_DIR=${CLUSTERS_DIR:-${DEMO_ROOT}/kubeconfig}
KCP_ROOT="${DEMO_ROOT}/kcp"

comment "Grab hub.kubeconfig"
mkdir -p kubeconfig
pe "kubectl config view --raw=true --minify=true --context=kind-hub > kubeconfig/hub.kubeconfig"

comment "Building kcp..."
if [ ! -d "${KCP_ROOT}" ]; then
    git clone https://github.com/qiujian16/kcp.git
fi
pushd $KCP_ROOT

git checkout rbac

make build
if [ ! -f "bin/kcp" ]; then
    echo "bin/kcp does not exist. Compilation probably filed"
    exit 1
fi
popd

comment "Building kcp-ocm..."
pushd $ROOT_DIR > /dev/null
make build
if [ ! -f "kcp-ocm" ]; then
    echo "kcp-ocm does not exist. Compilation probably filed"
    exit 1
fi
popd > /dev/null

comment "Validating ocm hub..."
export KUBECONFIG=${KUBECONFIG_DIR}/hub.kubeconfig
if [ ! -f "$KUBECONFIG" ]; then
    echo "$KUBECONFIG does not exist. Please generate kubeconfig for hub."
    exit 1
fi

unset KUBECONFIG

comment "Press any key to start KCP server and KCP_OCM controller"
wait

comment "Generate root ca"
openssl genrsa -out ${DEMO_ROOT}/rootca.key 2048
openssl req -x509 -new -nodes -key ${DEMO_ROOT}/rootca.key -sha256 -days 1024 -subj "/C=CN/ST=AA/L=AA/O=OCM/CN=OCM" -out ${DEMO_ROOT}/rootca.crt

comment "Starting KCP server ..."
(cd ${KCP_ROOT} && exec ./bin/kcp start --client-ca-file ../rootca.crt) &> kcp.log &
KCP_PID=$!
echo "KCP server started: $KCP_PID" 

kill -0 $KCP_PID
if [[ "$?" != 0 ]]; then
    echo "KCP not running check the kcp.log"
    exit 1
fi

echo "Waiting for KCP server to be up and running..." 
#TODO: smarter wait
sleep 10

comment "set external accessible IP for kcp kubeconfig"
kubectl --kubeconfig ${KCP_ROOT}/.kcp/admin.kubeconfig config set clusters.admin.server https://${KCPIP}:6443

comment "Starting KCP-OCM controller..."
${ROOT_DIR}/kcp-ocm manager \
    --kcp-kubeconfig="${KCP_ROOT}/.kcp/admin.kubeconfig" \
    --kubeconfig="${KUBECONFIG_DIR}/hub.kubeconfig" \
    --kcp-ca="${DEMO_ROOT}/rootca.crt" \
    --kcp-key="${DEMO_ROOT}/rootca.key" \
    --namespace=default &> kcp-ocm.log &
KCP_OCM_PID=$!
echo "KCP-OCM controller started: $KCP_OCM_PID"

echo "Waiting for KCP-OCM controller to be up and running..." 
sleep 10

kill -0 $KCP_OCM_PID
if [[ "$?" != 0 ]]; then
    echo "KCP-OCM not running check the kcp-ocm.log"
    cleanup
    exit 1 #TODO figure out how to trap exit and execute cleanup 
fi

comment "Press any key to stop KCP server and KCP_OCM controller"
wait 
