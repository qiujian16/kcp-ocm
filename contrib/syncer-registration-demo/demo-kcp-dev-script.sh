#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

source "${DEMO_DIR}"/demo-magic
source "${DEMO_DIR}"/utils

export KUBECONFIG=${DEMO_DIR}/.kcp/admin.kubeconfig
comment "As a KCP developer, I create a KCP workspace"
pe "kubectl apply -f workspace/workspace.yaml"

# comment "As a KCP admin, I assign this workspace to a kcp user"
# pe "cat ${DEMO_DIR}/workspace/clusterrole.yaml"
# pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole.yaml"
# pe "cat ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
# pe "kubectl apply -f ${DEMO_DIR}/workspace/clusterrole_binding.yaml"
# pe "kubectl get workspaces workspace1 -oyaml"
unset KUBECONFIG

export KUBECONFIG=${DEMO_DIR}/kubeconfig/hub.kubeconfig
comment "A namespace kcp-workspace1 will be created to correspond my workspace in the ACM hub"
pe "kubectl get ns -w"

comment "I have a clusterset in the ACM hub"
pe "kubectl get managedclusterset"

comment "I bind my clusterset to my workspace namespace in the ACM hub"
pe "kubectl -n kcp-workspace1 apply -f clusterset/clusterset_binding.yaml"
unset KUBECONFIG

comment "After I bound my clusterset, the kcp-syncer will be deployed to all managed clusters in my clusterset"

export KUBECONFIG=${DEMO_DIR}/kubeconfig/cluster1.kubeconfig
comment "kcp-syncer on the managed cluster cluster1"
pe "kubectl -n kcp-syncer-workspace1 get pods --watch"
unset KUBECONFIG

export KUBECONFIG=${DEMO_DIR}/kubeconfig/cluster2.kubeconfig
comment "kcp-syncer on the managed cluster cluster2"
pe "kubectl -n kcp-syncer-workspace1 get pods --watch"
unset KUBECONFIG
