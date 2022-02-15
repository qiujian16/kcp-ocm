1. setup a 1 hub/2 cluster environment with the cluster name as cluster1 and cluster2. Use script here https://github.com/open-cluster-management-io/OCM/tree/main/solutions/setup-dev-environment to setup on kind

2. Set the KCP external accessible IP
```script
export KCPIP=<kcp_external_accessible_ip>
```

3. run `./demo-setup.sh`

4. run `demo` in aother terminal
