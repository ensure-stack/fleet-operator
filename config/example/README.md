# How to get stuff working:

```bash
# Create Clusters
$ kind create cluster --name remote-1
$ kind create cluster --name fleet-command

# Create Kubeconfig Secret to talk with the "other" cluster
$ kind get kubeconfig --internal --name remote-1 > remote-1.kubeconfig
$ IP=$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' remote-1-control-plane)
# something wonky with in-cluster DNS within docker
$ sed -i "s/remote-1-control-plane/$IP/" remote-1.kubeconfig
$ kubectl create secret generic remote-1-kubeconfig --type='ensure-stack.org/kubeconfig' --from-file=kubeconfig=remote-1.kubeconfig

# Run Controller (new terminal)
$ export KUBECONFIG=~/.kube/config
$ make run-fleet-operator-manager

# Register the Remote Cluster
$ kubectl apply -f config/example/remote-cluster.yaml

# Create a deployment on the remote cluster:
$ kubectl apply -f config/example/remote-deployment.yaml

# MAGIC!
```
