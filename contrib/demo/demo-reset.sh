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
    kubectl delete $managedcluster --wait=false
done

#remove managedclusterset
kubectl delete managedclusterset demo-managedclusterset

#remove demo namespace
kubectl delete ns demo

rm -rf *.log

sleep 60

for managedcluster in `kubectl get managedcluster -o custom-columns=NAME:.metadata.name --no-headers | grep demo-managedcluster`; do
    kubectl patch managedcluster ${managedcluster} --type json -p '[{ "op": "remove", "path": "/metadata/finalizers" }]'
done

for ns in `kubectl get ns -o custom-columns=NAME:.metadata.name --no-headers | grep demo-managedcluster`; do
    for manifestwork in `kubectl get manifestwork -n ${ns} -o custom-columns=NAME:.metadata.name --no-headers`; do
        kubectl patch manifestwork -n ${ns} ${manifestwork} --type json -p '[{ "op": "remove", "path": "/metadata/finalizers" }]'
    done

    for rolebinding in `kubectl get rolebinding -n ${ns} -o custom-columns=NAME:.metadata.name --no-headers`; do
        kubectl patch rolebinding -n ${ns} $rolebinding --type json -p '[{ "op": "remove", "path": "/metadata/finalizers" }]'
    done
done