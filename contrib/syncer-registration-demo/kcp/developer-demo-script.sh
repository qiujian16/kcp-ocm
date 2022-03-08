#!/bin/bash

export KCP_ACM_INTEGRATION_NAMESPACE=${KCP_ACM_INTEGRATION_NAMESPACE:-"kcp-demo"}

DEMO_DIR="$(dirname "${BASH_SOURCE[0]}")"
ROOT_DIR="$(cd ${DEMO_DIR}/.. && pwd)"
KUBECONFIG_DIR=${ROOT_DIR}/kubeconfig

. ../${DEMO_DIR}/demo-magic

function comment() {
  echo -e '\033[0;33m>>> '$1' <<<\033[0m'
}

clear

export KUBECONFIG=${KUBECONFIG_DIR}/hub.kubeconfig
if [ "$(uname)" = "Darwin" ]; then
  export KCPIP=$(ifconfig en0 | grep inet | grep -v inet6 | awk '{print $2}')
else
  export KCPIP=$(ifconfig eth0 | grep inet | grep -v inet6 | awk '{print $2}')
fi
comment "As a KCP developer, I deploy my kcp server in the namesapce ${KCP_ACM_INTEGRATION_NAMESPACE} on the ACM"
pe "./deploy-script.sh"

comment "Wait for exporting the kcp service on external accessible IP ${KCPIP}"
#TODO: smarter wait for kcp-server was deployed and start
sleep 10

# kubectl port-forward -n kcp-demo service/kcp 6443 --address=10.0.118.32
kubectl port-forward -n kcp-demo service/kcp 6443 --address=${KCPIP} >> kcp-port-forward.log &

kubectl -n ${KCP_ACM_INTEGRATION_NAMESPACE} get secrets kcp-admin-kubeconfig -o=jsonpath="{.data.admin\.kubeconfig}" | base64 -d > ${KUBECONFIG_DIR}/admin.kubeconfig
unset KUBECONFIG

export KUBECONFIG=${KUBECONFIG_DIR}/admin.kubeconfig
# prepare kcp kubeconfig for workspaceshard
kubectl get namespace default &> /dev/null || kubectl create namespace default
kubectl create secret generic kubeconfig --from-file=kubeconfig=${KUBECONFIG}

comment "As a KCP developer, I create a WorkspaceShard that corresponds to my KCP server"
pe "kubectl apply -f ${DEMO_DIR}/workspace/workspaceshard.yaml"

comment "As a KCP developer, I create a KCP workspace"
pe "kubectl apply -f ${DEMO_DIR}/workspace/workspace.yaml"

# comment "As a KCP admin, I assign this workspace to a kcp user"
# pe "cat ${DEMO_DIR}/workspace/clusterrole.yaml"
# pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole.yaml"
# pe "cat ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
# pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
# pe "kubectl get workspaces workspace1 -oyaml"
unset KUBECONFIG

export KUBECONFIG=${KUBECONFIG_DIR}/hub.kubeconfig
comment "As a KCP developer, I link my ManagedClusterSet with the KCP workspace on the ACM hub"
pe "kubectl annotate managedclusterset dev \"kcp-workspace=workspace1\" --overwrite"
unset KUBECONFIG

comment "After linked my ManagedClusterSet, the kcp-syncer will be deployed to all managed clusters in my ManagedClusterSet"

export KUBECONFIG=${KUBECONFIG_DIR}/cluster1.kubeconfig
comment "kcp-syncer on the managed cluster cluster1"
pe "kubectl -n kcp-syncer-${KCP_ACM_INTEGRATION_NAMESPACE}-workspace1 get pods --watch"
unset KUBECONFIG

export KUBECONFIG=${KUBECONFIG_DIR}/cluster2.kubeconfig
comment "kcp-syncer on the managed cluster cluster2"
pe "kubectl -n kcp-syncer-${KCP_ACM_INTEGRATION_NAMESPACE}-workspace1 get pods --watch"
unset KUBECONFIG
