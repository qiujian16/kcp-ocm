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

this script will first compile `kcp` and `kcp-ocm` as well as download `cm-cli` for open-cluster-management

the script will than wait for user key press to start both `kcp` and `kcp-ocm`
`kcp` and `kcp-ocm` will be left running till another user key press is recieved 

leave this script running while running the demo than stop the script by pressing any key

## Running the demo
```
./demo -n
```
the demo will wait for key press to proceed after each section 

TODO: add more description about what's shown in the demo
