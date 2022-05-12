#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

source "${DEMO_DIR}"/demo-magic
source "${DEMO_DIR}"/utils

clear

export KUBECONFIG=${DEMO_DIR}/.kcp/demo.kubeconfig
comment "Create an organization workspace in the KCP server"
pe "kubectl kcp workspace create acm --type Organization"

comment "Create a workspace for location in acm workspace"
pe "kubectl kcp workspace use acm"
pe "kubectl kcp workspace create location"

comment "Create a workspace for user in acm workspace"
pe "kubectl kcp workspace create user"
unset KUBECONFIG

comment "There is a clusterset in the OCM hub"
pe "kubectl get managedclusterset,managedclusters --show-labels --kubeconfig kubeconfig/hub.kubeconfig"

comment "Link the clusterset dev to the kcp location workspace"
pe "kubectl annotate managedclusterset dev \"kcp-workspace=acm:location\" --overwrite --kubeconfig kubeconfig/hub.kubeconfig"

export KUBECONFIG=${DEMO_DIR}/.kcp/demo.kubeconfig
comment "After the clusterset was annotated, the workload clusters will be created to the location workspace to correspond to the managed clusters"
pe "kubectl kcp workspace use location"
pe "kubectl get workloadclusters"
unset KUBECONFIG

comment "and the kcp-syncer will be deployed to all managed clusters in the clusterset dev"
comment "kcp-syncer on the managed cluster cluster1"
pe "kubectl -n kcp-syncer-acm-location get pods --watch --kubeconfig kubeconfig/cluster1.kubeconfig"
comment "kcp-syncer on the managed cluster cluster2"
pe "kubectl -n kcp-syncer-acm-location get pods --watch --kubeconfig kubeconfig/cluster2.kubeconfig"

export KUBECONFIG=${DEMO_DIR}/.kcp/demo.kubeconfig
comment "After the kcp-syncer was deployed, the resource will be imported from managed clusters to location workspace"
pe "kubectl kcp workspace current"
pe "kubectl get apiresourceimports -o wide"
pe "kubectl get negotiatedapiresources -o wide"
pe "kubectl api-resources | grep deployments"

# comment 'Sync a deployment from a kcp workspace to a managed cluster'
# pe "kubectl apply -f deployment/nginx.yaml"
# pe "kubectl -n nginx get deployment --watch"
# pe "kubectl -n nginx get deployment --show-labels"

comment "Create a location in location workspace"
pe "kubectl apply -f scheduling/location.yaml"
pe "kubectl get location.scheduling.kcp.dev/clusters"

comment "Export the deployment api in location workspace"
pe "kubectl apply -f scheduling/deployment_apiexport.yaml"

comment "Bind the deployment api in user workspace"
pe "kubectl config use-context root"
pe "kubectl kcp workspace use acm:user"
pe "kubectl apply -f scheduling/deployment_apibinding.yaml"
pe "kubectl get apibinding.apis.kcp.dev/deployment -oyaml"
pe "kubectl get ns default -oyaml"

unset KUBECONFIG
