#!/bin/bash

KCP_ACM_CTRL_NAMESPACE=${KCP_ACM_CTRL_NAMESPACE:-"default"}

function comment() {
  echo -e '\033[0;33m>>> '$1' <<<\033[0m'
}

DEMO_DIR="$(dirname "${BASH_SOURCE[0]}")"
ROOT_DIR="$(cd ${DEMO_DIR}/.. && pwd)"
KUBECONFIG_DIR=${ROOT_DIR}/kubeconfig

. ../${DEMO_DIR}/demo-magic

# prepare acm kubeconfigs and validate acm env
rm -rf ${KUBECONFIG_DIR}
mkdir -p ${KUBECONFIG_DIR}

kubectl config view --minify --flatten --context=kind-hub > ${KUBECONFIG_DIR}/hub.kubeconfig
kubectl config view --minify --flatten --context=kind-cluster1 > ${KUBECONFIG_DIR}/cluster1.kubeconfig
kubectl config view --minify --flatten --context=kind-cluster2 > ${KUBECONFIG_DIR}/cluster2.kubeconfig

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

clean

# start demo
comment "Create a clusterset and add managed clusters to it"
pe "kubectl apply -f resources/clusterset.yaml"
pe "kubectl label managedclusters cluster1 cluster.open-cluster-management.io/clusterset=dev --overwrite"
pe "kubectl label managedclusters cluster2 cluster.open-cluster-management.io/clusterset=dev --overwrite"

comment "Bind this clusterset to namespace ${KCP_ACM_CTRL_NAMESPACE}"
pe "kubectl -n ${KCP_ACM_CTRL_NAMESPACE} apply -f resources/clusterset_binding.yaml"
unset KUBECONFIG
