# Kubernetes manifests

The Widget resource type and a sample Widget object, shared by every Kubernetes operator in this repository. Everything is in the namespace `default`.

| file | contains | needed |
|---|---|---|
| [00-crd.yaml](00-crd.yaml) | the Widget CustomResourceDefinition (CRD): `spec.replicas`, and `status` as a subresource holding the result `{replicas, ready}` | always |
| [01-widget-demo.yaml](01-widget-demo.yaml) | the Widget `"demo"`, spec `replicas: 3` | always |
| [02-rbac.yaml](02-rbac.yaml) | the operator's ServiceAccount, and a Role and RoleBinding allowing "LIST" and "WATCH" on Widgets and `PATCH` on their status | only when the operator runs in a Pod; the images and the Deployment for that are not part of this project yet |

## Run an operator against a cluster

Any cluster where you may install a CRD works, eg kind, which runs Kubernetes in Docker. From this directory:

```bash
kind create cluster
kubectl apply -f 00-crd.yaml
kubectl wait --for=condition=Established crd/widgets.example.com   # Until then, Widget objects are rejected.
kubectl apply -f 01-widget-demo.yaml
kubectl proxy &    # Listens on 127.0.0.1:8001 and adds your kubeconfig credentials.
```

Then start an operator as its readme says ([Python](../levels/2_structured/python/readme.md#run-the-kubernetes-operator), [Go](../levels/2_structured/go/readme.md#run-the-kubernetes-operator)), and follow the result in a second terminal:

```bash
kubectl get widget demo --watch
# NAME   SPEC   RUNNING   READY
# demo   3      1         false
```

Change the spec with `kubectl patch widget demo --type merge -p '{"spec": {"replicas": 1}}'`, and delete it with `kubectl delete widget demo`.

## Remove it

```bash
kubectl delete -f . --ignore-not-found    # Deleting the CRD also deletes every Widget object.
kind delete cluster
```
