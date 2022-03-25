#!/usr/bin/env bash

unset KUBECONFIG

rm -rf kubeconfig
rm -rf .kcp
rm -f rootca.*
rm -f *.log
rm -f kcp-started
rm -f kcp.tokens

kubectl config use-context kind-hub
kubectl -n cluster1 delete managedclusteraddons --all
kubectl -n cluster2 delete managedclusteraddons --all

kubectl delete managedclustersetbindings --all --all-namespaces

kubectl label managedclusters cluster1 cluster.open-cluster-management.io/clusterset- --overwrite
kubectl label managedclusters cluster2 cluster.open-cluster-management.io/clusterset- --overwrite
kubectl delete managedclustersets --all

kubectl get clustermanagementaddons | grep -v NAME
if [ "$?" == 0 ]; then
    kubectl get clustermanagementaddons | grep -v NAME | awk '{print $1}' | xargs kubectl patch clustermanagementaddons -p '{"metadata":{"finalizers": []}}' --type=merge
fi
kubectl delete clustermanagementaddons.addon.open-cluster-management.io --all

kubectl delete namespace kcp-workspace1
