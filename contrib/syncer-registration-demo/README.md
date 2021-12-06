1. build kcp using `https://github.com/qiujian16/kcp/tree/rbac`

2. create client ca and key so controller can sign csr

```
openssl genrsa -out rootca.key 2048
openssl req -x509 -new -nodes -key rootca.key -sha256 -days 1024 -out rootca.crt
```

3. start kcp with client ca enabled

```
./bin/kcp start --install_cluster_controller --client-ca-file rootca.crt
```

4. start the controller with the following command
```
./kcp-ocm manager --kubeconfig <hub kubeconfig> --kcp-ca rootca.crt --kcp-key rootca.key --kcp-kubeconfig <kcp-kubeconfig> --namespace default
```
Note: ensure the kcp-kubconfig has the server address that can be reachable from spoke to the kcp server.

2. Create a clustermanagementaddon resource from cm.yaml on the hub. It indicates a workspace with the name of "test" on kcp

3. Add annotation "kcp-lcluster: test" to a managedcluster