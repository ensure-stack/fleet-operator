
## Create Clusters

```bash
$ kind create cluster --name fleet-command
$ kind create cluster --name remote-1

$ kind get kubeconfig --internal --name remote-1 > remote-1.kubeconfig
$ IP=$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' remote-1-control-plane)
# something wonky with in-cluster DNS within docker
$ sed -i "s/remote-1-control-plane/$IP/" remote-1.kubeconfig
$ kubectl create secret generic remote-1-kubeconfig --type='ensure-stack.org/kubeconfig' --from-file=kubeconfig=remote-1.kubeconfig

```
