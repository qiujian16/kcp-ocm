# KCP-OCM Demo (first draft)

The `demo` shows a prototype using `kcp` API server with `kcp-ocm` controller to demonstrate synergy between existing open-cluster-management concepts with KCP concept of "transparent multicluster"

## Pre-req for demo
### 1. open-cluster-management hub
place kubeconfig in `contrib/demo/kubeconfig` as `hub.kubeconfig`

TODO: add install instruction for ACM

### 2. at least 2 kubernetes cluster
place the seperate kubeconfig files for the cluster in `contrib/demo/kubeconfig/managedcluster`

## Setup the demo
```
./demo-setup.sh
```

## Running the demo
```
./demo -n
```

TODO: add more description about what's shown in the demo
