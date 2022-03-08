#!/bin/bash
export KCP_ACM_INTEGRATION_NAMESPACE=${KCP_ACM_INTEGRATION_NAMESPACE:-"kcp-demo"}
export KCPIP="placeholder"

rm -rf kubeconfig
rm -f acm/rootca.*
rm -f kcp/*.log

#ps ax | grep port-forward | grep -v grep | awk '{print $1}' | xargs kill

kubectl config use-context kind-hub
kubectl -n cluster1 delete managedclusteraddons --all
kubectl -n cluster2 delete managedclusteraddons --all

kubectl -n ${KCP_ACM_INTEGRATION_NAMESPACE} delete managedclustersetbindings --all

kubectl label managedclusters cluster1 cluster.open-cluster-management.io/clusterset- --overwrite
kubectl label managedclusters cluster2 cluster.open-cluster-management.io/clusterset- --overwrite
kubectl delete managedclustersets --all

kubectl get clustermanagementaddons | grep -v NAME
if [ "$?" == 0 ]; then
    kubectl get clustermanagementaddons | grep -v NAME | awk '{print $1}' | xargs kubectl patch clustermanagementaddons -p '{"metadata":{"finalizers": []}}' --type=merge
fi
kubectl delete clustermanagementaddons.addon.open-cluster-management.io --all

acm/deploy-script.sh clean

kcp/deploy-script.sh clean

kubectl delete namespace ${KCP_ACM_INTEGRATION_NAMESPACE}
