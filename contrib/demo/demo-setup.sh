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

#install CM-CLI (because it make the demo pretty)
comment "Getting cm-cli..."
#TODO: make this multi-arch
if [ ! -f "kubectl-cm" ]; then
    wget -qO- https://github.com/open-cluster-management/cm-cli/releases/download/v1.0.0-beta.4/cm_darwin_amd64.tar.gz | tar zxvf -
    mv cm kubectl-cm
fi

comment "Building kcp..."
if [ ! -d "${KCP_ROOT}" ]; then
    git clone https://github.com/kcp-dev/kcp.git
fi
pushd $KCP_ROOT

if [ ! -z ${KCP_GIT_SHA} ]; then 
    git checkout ${KCP_GIT_SHA}
fi

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

kubectl cluster-info

if [[ "$(kubectl get mch | grep Running | wc -l | xargs)" != "1" ]]; then
    echo "No multiclusterhub running. Please configure a multiclusterhub before executing this script. Exiting..."
    exit 1
fi

if [[ "$(kubectl get managedcluster --no-headers 2> /dev/null | wc -l | xargs)" != "0" ]]; then
    echo "Expecting a clean hub for the demo."
   exit 1
fi
unset KUBECONFIG

comment "Validating managed clusters..."
managedcluster_count=0
for file in $(ls ${KUBECONFIG_DIR}/managedclusters/*); do
    export KUBECONFIG=$file
    kubectl cluster-info
    if [[ "$?" != 0 ]]; then
        echo "BAD KUBECONFIG for ManagedCluster $file"
        exit 1
    fi
    managedcluster_count=$((managedcluster_count+1))
done

if [[ "$managedcluster_count" != "2" ]]; then
    echo "Expect 2 managedcluster for demo"
    exit 1
fi

comment "Press any key to start KCP server and KCP_OCM controller"
wait

comment "Starting KCP server ..."
(cd ${KCP_ROOT} && exec ./bin/kcp start) &> kcp.log &
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

comment "Adding deployment to KCP"
export KUBECONFIG="${KCP_ROOT}/.kcp/data/admin.kubeconfig"
kubectl config view --minify=true --raw=true > ${KUBECONFIG_DIR}/kcp/admin.kubeconfig
kubectl config view --minify=true --raw=true | sed 's/\:6443/\:6443\/clusters\/demo/g' > ${KUBECONFIG_DIR}/kcp/demo.kubeconfig
kubectl apply -f ${DEMO_ROOT}/resources/deployments.apps.yaml
export KUBECONFIG=${KUBECONFIG_DIR}/kcp/demo.kubeconfig
kubectl apply -f ${DEMO_ROOT}/resources/deployments.apps.yaml

comment "Starting KCP-OCM controller..."
${ROOT_DIR}/kcp-ocm agent \
    --kcp-kubeconfig="${KCP_ROOT}/.kcp/data/admin.kubeconfig" \
    --kubeconfig="${KUBECONFIG_DIR}/hub.kubeconfig" \
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
