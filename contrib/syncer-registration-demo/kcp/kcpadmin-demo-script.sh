#!/bin/bash

# The namespace in which the kcp server will be installed
KCP_SERVER_NAMESPACE=${KCP_SERVER_NAMESPACE:-"default"}

DEMO_DIR="$(dirname "${BASH_SOURCE[0]}")"
. ../${DEMO_DIR}/demo-magic

function comment() {
  echo -e '\033[0;33m>>> '$1' <<<\033[0m'
}

#TODO: prepare the admin.kubeconfig
# kubectl port-forward -n kcp-demo service/kcp 6443 --address=10.0.118.32

kubectl -n kcp-demo get secrets kcp-admin-kubeconfig -o=jsonpath="{.data.admin\.kubeconfig}" | base64 -d > admin.kubeconfig

export KUBECONFIG=${DEMO_DIR}/admin.kubeconfig

kubectl get namespace default &> /dev/null || kubectl create namespace default

clear

comment "As a KCP admin, I create a WorkspaceShard that corresponds to the current KCP server"
pe "kubectl create secret generic kubeconfig --from-file=kubeconfig=${KUBECONFIG}"
pe "kubectl apply -f ${DEMO_DIR}/workspace/workspaceshard.yaml"

comment "As a KCP admin, I create a KCP workspace"
pe "cat ${DEMO_DIR}/workspace/workspace.yaml"
pe "kubectl apply -f ${DEMO_DIR}/workspace/workspace.yaml"

comment "As a KCP admin, I assign this workspace to a developer"
pe "cat ${DEMO_DIR}/workspace/clusterrole.yaml"
pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole.yaml"
pe "cat ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
pe "kubectl get workspaces workspace1 -oyaml"

unset KUBECONFIG
