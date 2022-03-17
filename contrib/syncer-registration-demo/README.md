1. Setup a 1 hub/2 cluster environment with the cluster name as cluster1 and cluster2. Use script here https://github.com/open-cluster-management-io/OCM/tree/main/solutions/setup-dev-environment to setup on kind

2. Setup demo enviroment
    - run `./startKCPServer.sh` to start KCP server
    - run `./startACMController.sh` to start kcp-acm controller in aother terminal

3. Run the demo script `./demo-kcp-dev-script.sh` in aother terminal
