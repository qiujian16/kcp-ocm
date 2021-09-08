#!/bin/bash

DEMO_ROOT="$( dirname "${BASH_SOURCE[0]}" )"
ROOT_DIR="$( cd ${DEMO_ROOT}/../.. && pwd)"
KUBECONFIG_DIR=${CLUSTERS_DIR:-${DEMO_ROOT}/kubeconfig}
KCP_ROOT="${DEMO_ROOT}/kcp"

#clear out KCP
rm -rf ${KCP_ROOT}/.kcp

export KUBECONFIG=${KUBECONFIG_DIR}/hub.kubeconfig

#remove managedcluster
for managedcluster in `kubectl get managedcluster -o name | grep demo-managedcluster`; do
    kubectl delete $managedcluster
done

#remove managedclusterset
kubectl delete managedclusterset demo-managedclusterset

#remove demo namespace
kubectl delete ns demo
