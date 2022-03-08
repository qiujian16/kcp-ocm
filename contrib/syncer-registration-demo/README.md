1. setup a 1 hub/2 cluster environment with the cluster name as cluster1 and cluster2. Use script here https://github.com/open-cluster-management-io/OCM/tree/main/solutions/setup-dev-environment to setup on kind

2. As a ACM SRE, run the `sre-demo-script.sh` in the `acm` directory, this will
    - deploy the kcp-acm integration controller in the `kcp-demo` namesapce on the ACM hub
    - create clusterset and bind this clusterset to the `kcp-demo` namesapce

3. As a KCP developer, run `developer-demo-script.sh` in the `kcp` deirecory in aother terminal, this will
    - deploy the kcp server in the `kcp-demo` namesapce on the ACM hub
    - create kcp workspace
    - link the created kcp workspace with a clusterset
