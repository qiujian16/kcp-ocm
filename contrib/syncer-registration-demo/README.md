1. start the controller with the following command
```
./kcp-ocm manager --kubeconfig <hub kubeconfig> --kcp-ca <kcp-ca> --kcp-key <kcp-ca-key> --kcp-kubeconfig <kcp-kubeconfig> --namespace default
```
2. Create a clustermanagementaddon resource from cm.yaml on the hub. It indicates a workspace with the name of "test" on kcp
3. Add annotation "kcp-lcluster: test" to a managedcluster