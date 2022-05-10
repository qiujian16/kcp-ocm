#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

source "${DEMO_DIR}"/demo-magic
source "${DEMO_DIR}"/utils

clear

export KUBECONFIG=${DEMO_DIR}/.kcp/demo.kubeconfig
comment "Create an organization workspace in the KCP server"
pe "kubectl kcp workspace create acm --type Organization"

comment "Create a negotiation workspace in acm workspace"
pe "kubectl kcp workspace use acm"
pe "kubectl kcp workspace create dev"
# todo tag this workspace
unset KUBECONFIG

comment "A namespace that corresponds the kcp workspace will be created in the OCM hub"
pe "kubectl get ns --watch --kubeconfig kubeconfig/hub.kubeconfig"

comment "There is a clusterset in the OCM hub"
pe "kubectl get managedclusterset,managedclusters --show-labels --kubeconfig kubeconfig/hub.kubeconfig"
comment "Bind the clusterset to the workspace namespace in the OCM hub"
pe "kubectl -n kcp-acm-dev apply -f clusterset/clusterset_binding.yaml --kubeconfig kubeconfig/hub.kubeconfig"

comment "After the clusterset wat bound, the kcp-syncer will be deployed to all managed clusters in the clusterset"
comment "kcp-syncer on the managed cluster cluster1"
pe "kubectl -n kcp-syncer-acm-dev get pods --watch --kubeconfig kubeconfig/cluster1.kubeconfig"
comment "kcp-syncer on the managed cluster cluster2"
pe "kubectl -n kcp-syncer-acm-dev get pods --watch --kubeconfig kubeconfig/cluster2.kubeconfig"

export KUBECONFIG=${DEMO_DIR}/.kcp/demo.kubeconfig
comment 'Sync a deployment from a KCP workspace to a managed cluster'
pe "kubectl kcp workspace use dev"
pe "kubectl apply -f deployment/nginx.yaml"
pe "kubectl -n nginx get deployment --watch"
pe "kubectl -n nginx get deployment --show-labels"
unset KUBECONFIG

# starting splitter for test ...
# (cd "${DEMO_DIR}" && exec ${DEMO_DIR}/kcp/bin/deployment-splitter --kubeconfig ${DEMO_DIR}/.kcp/workspace.kubeconfig) &> splitter.log &


# pe "kubectl apply -f deployment/nginx.yaml --kubeconfig .kcp/workspace.kubeconfig"
# pe "kubectl -n nginx get deployment --watch --kubeconfig .kcp/workspace.kubeconfig"
