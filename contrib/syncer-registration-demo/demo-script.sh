#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

source "${DEMO_DIR}"/demo-magic
source "${DEMO_DIR}"/utils

clear

comment "Create a KCP workspace in the KCP server"
pe "kubectl apply -f workspace/workspace.yaml --kubeconfig .kcp/admin.kubeconfig"

# comment "As a KCP admin, I assign this workspace to a kcp user"
# pe "cat ${DEMO_DIR}/workspace/clusterrole.yaml"
# pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole.yaml"
# pe "cat ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
# pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
# pe "kubectl get workspaces workspace1 -oyaml"

comment "A namespace that corresponds the kcp workspace will be created in the OCM hub"
pe "kubectl get ns --kubeconfig kubeconfig/hub.kubeconfig -w"

comment "There is a clusterset in the OCM hub"
pe "kubectl get managedclusterset,managedclusters --show-labels --kubeconfig kubeconfig/hub.kubeconfig"
comment "Bind the clusterset to the workspace namespace in the OCM hub"
pe "kubectl -n kcp-workspace1 apply -f clusterset/clusterset_binding.yaml --kubeconfig kubeconfig/hub.kubeconfig"

comment "After the clusterset wat bound, the kcp-syncer will be deployed to all managed clusters in the clusterset"
comment "kcp-syncer on the managed cluster cluster1"
pe "kubectl -n kcp-syncer-workspace1 get pods --watch --kubeconfig kubeconfig/cluster1.kubeconfig"
comment "kcp-syncer on the managed cluster cluster2"
pe "kubectl -n kcp-syncer-workspace1 get pods --watch --kubeconfig kubeconfig/cluster2.kubeconfig"

export KUBECONFIG=${DEMO_DIR}/.kcp/admin.kubeconfig
kubectl config view --minify --flatten | sed 's/root\:default/default\:workspace1/g' > ${DEMO_DIR}/.kcp/workspace.kubeconfig
unset KUBECONFIG

# starting splitter for test ...
(cd "${DEMO_DIR}" && exec ${DEMO_DIR}/kcp/bin/deployment-splitter --kubeconfig ${DEMO_DIR}/.kcp/workspace.kubeconfig) &> splitter.log &
SPLITTER_PID=$!
echo $SPLITTER_PID

comment 'Create a deployment in the KCP workspace'
pe "kubectl apply -f deployment/nginx.yaml --kubeconfig .kcp/workspace.kubeconfig"
pe "kubectl -n nginx get deployment --watch --kubeconfig .kcp/workspace.kubeconfig"
