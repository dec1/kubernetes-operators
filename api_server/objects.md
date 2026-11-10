# Detail: resource types, objects and status

**A resource type describes a structure; an object is one instance of it with particular values.** This page covers the terms that [Operator and API server](readme.md) uses without explaining.

- **Resource type**: the structure its objects follow, analogous to a class. Example: Widget.
- **CustomResourceDefinition (CRD)**: the document that defines a custom resource type: its fields, their value types and rules. Example: [manifests/00-crd.yaml](../manifests/00-crd.yaml). A CRD is itself a stored object; its name `widgets.example.com` is distinct from the name of a Widget object.
- **Object**: one instance. Example: the Widget `demo` in the namespace `default`, with `spec.replicas: 3`, from [manifests/01-widget-demo.yaml](../manifests/01-widget-demo.yaml).

## Who defines a field, and who sets its value

**Declaring a field and supplying its value are separate.** Kubernetes defines the standard fields, the CRD adds the custom ones:

| fields | defined by | value set by |
|---|---|---|
| `apiVersion`, `kind`, `metadata.name`, `metadata.namespace` | Kubernetes | the client |
| `metadata.uid`, `metadata.resourceVersion`, `metadata.generation` | Kubernetes | the API server |
| `spec.replicas` | the Widget CRD | the client |
| `status.replicas`, `status.ready` | the Widget CRD | the operator |

The API server checks each write against the CRD's rules before storing it. A Widget without `spec.replicas`, or with a negative value, is rejected there; no operator code runs.

Three fields the API server sets matter to an operator:

| field | meaning | used for |
|---|---|---|
| `metadata.resourceVersion` | the position of this object's latest change | where the next watch starts |
| `metadata.generation` | a counter that grows when `spec` changes, but not when `status` is written | the library controller skips notifications that leave it unchanged, eg its own status `PATCH` |
| `metadata.deletionTimestamp` | set when deletion was requested but cannot finish yet | the operators treat such an object as deleted |

## What identifies an object

**API group + resource type + namespace + name.** For the sample Widget: `example.com`, `widgets`, `default`, `demo`. The API version (`v1`) is not part of the identity, and cluster-scoped resource types, eg Node and CRD, have no namespace.

| `kubectl apply` of an object's YAML | result |
|---|---|
| no object with this identity | the object is created, with a new `metadata.uid` |
| an object with this identity exists | that object is updated; its `uid` stays; an unchanged spec changes nothing |
| it existed and was deleted | a new object is created, with a new `uid`, although the name is reused |

## Status is a report

**`status` is what the operator last reported, not the internal state itself.** It can be partial or out of date.

A built-in example: for a ReplicaSet, `spec.replicas: 3`, `status.replicas: 3` and `status.readyReplicas: 2` mean that three Pods exist but only two were ready at the last check. Built-in controllers also build on each other in this way: the Deployment controller writes ReplicaSet objects, the ReplicaSet controller creates Pods, and each kubelet reports the status of its Pods.

References: [CRDs: schemas and status](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/), [object names and UIDs](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/), [ReplicaSets](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/).
