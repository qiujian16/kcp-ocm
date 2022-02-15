#!/bin/bash

trap cleanup 1 2 3 6

hubcleanup() {
    kubectl config use-context kind-hub
    kubectl -n cluster1 delete managedclusteraddons --all
    kubectl -n cluster2 delete managedclusteraddons --all

    kubectl label managedclusters cluster1 cluster.open-cluster-management.io/clusterset- --overwrite
    kubectl label managedclusters cluster2 cluster.open-cluster-management.io/clusterset- --overwrite
    kubectl delete managedclustersets --all

    kubectl get clustermanagementaddons | grep -v NAME
    if [ "$?" == 0 ]; then
        kubectl get clustermanagementaddons | grep -v NAME | awk '{print $1}' | xargs kubectl patch clustermanagementaddons -p '{"metadata":{"finalizers": []}}' --type=merge
    fi
    kubectl delete clustermanagementaddons.addon.open-cluster-management.io --all
}

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

rm -f *.log

rm -rf kubeconfig
mkdir -p kubeconfig

comment "Validating ocm hub..."
kubectl config view --minify --flatten --context=kind-hub > kubeconfig/hub.kubeconfig
kubectl config view --minify --flatten --context=kind-cluster1 > kubeconfig/cluster1.kubeconfig
kubectl config view --minify --flatten --context=kind-cluster2 > kubeconfig/cluster2.kubeconfig
export KUBECONFIG=${KUBECONFIG_DIR}/hub.kubeconfig
if [ ! -f "$KUBECONFIG" ]; then
    echo "$KUBECONFIG does not exist. Please generate kubeconfig for hub."
    exit 1
fi
kubectl get managedclusters
if [[ "$?" != 0 ]]; then
    echo "Failed to apply managed cluster set on the hub cluster."
    unset KUBECONFIG
    exit 1
fi
# ensure a clear env
hubcleanup

# create a demo clusterset and add managed cluster to it
kubectl apply -f clusterset.yaml
kubectl label managedclusters cluster1 cluster.open-cluster-management.io/clusterset=demo --overwrite
kubectl label managedclusters cluster2 cluster.open-cluster-management.io/clusterset=demo --overwrite

unset KUBECONFIG

comment "Building kcp..."
if [ ! -d "${KCP_ROOT}" ]; then
    git clone https://github.com/skeeey/kcp
fi

pushd $KCP_ROOT
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

comment "Press any key to start KCP server and KCP_OCM controller"
wait

comment "Generate root ca"
openssl genrsa -out ${DEMO_ROOT}/rootca.key 2048
openssl req -x509 -new -nodes -key ${DEMO_ROOT}/rootca.key -sha256 -days 1024 -subj "/C=CN/ST=AA/L=AA/O=OCM/CN=OCM" -out ${DEMO_ROOT}/rootca.crt

echo "dev-user-token,dev-user,1111-1111-1111-1111,\"dev-team\"" > kcp.tokens

comment "Starting KCP server ..."
rm -rf ${KCP_ROOT}/.kcp
(cd ${KCP_ROOT} && exec ./bin/kcp start --install-workspace-scheduler --client-ca-file ../rootca.crt --token-auth-file ../kcp.tokens) &>> kcp.log &
KCP_PID=$!
echo "KCP server started: $KCP_PID" 

kill -0 $KCP_PID
if [[ "$?" != 0 ]]; then
    echo "KCP not running check the kcp.log"
    exit 1
fi

comment "Waiting for KCP server to be up and running..."
#TODO: smarter wait
sleep 10

comment "Ensure the default namespace in the kcp"
export KUBECONFIG=${KCP_ROOT}/.kcp/admin.kubeconfig
#workaround for a kcp issue https://github.com/kcp-dev/kcp/issues/157 
kubectl get namespace default &> /dev/null || kubectl create namespace default
#end workaround
unset KUBECONFIG

comment "Set external accessible IP for kcp kubeconfig"
kubectl --kubeconfig ${KCP_ROOT}/.kcp/admin.kubeconfig config set clusters.admin.server https://${KCPIP}:6443

comment "Starting KCP-OCM controller..."
${ROOT_DIR}/kcp-ocm manager \
    --kcp-kubeconfig="${KCP_ROOT}/.kcp/admin.kubeconfig" \
    --kubeconfig="${KUBECONFIG_DIR}/hub.kubeconfig" \
    --kcp-ca="${DEMO_ROOT}/rootca.crt" \
    --kcp-key="${DEMO_ROOT}/rootca.key" \
    --namespace=default &>> kcp-ocm.log &
KCP_OCM_PID=$!
echo "KCP-OCM controller started: $KCP_OCM_PID"

echo "Waiting for KCP-OCM controller to be up and running..." 
sleep 10

kill -0 $KCP_OCM_PID
if [[ "$?" != 0 ]]; then
    echo "KCP-OCM not running check the kcp-ocm.log"
    kill $KCP_PID
    exit 1
fi

comment "Press any key to stop KCP server and KCP_OCM controller"
wait

cleanup
