# Detail: access from a Pod

**An operator running in a Pod calls the API server as a ServiceAccount, and may make exactly the calls a Role permits to that ServiceAccount.** Run outside the cluster, as in this repository's readmes, it uses your own `kubectl` permissions instead, and none of this is needed.

[manifests/02-rbac.yaml](../manifests/02-rbac.yaml) supplies three objects, all named `widget-operator`, in the namespace `default`:

| object | role |
|---|---|
| **ServiceAccount** | the identity the Pod uses when calling the API server |
| **Role** | permits `list` and `watch` on `widgets`, and `patch` on `widgets/status`, in the API group `example.com` |
| **RoleBinding** | grants that Role to that ServiceAccount |

The Role permits exactly the [three calls](readme.md) and nothing else: the operator cannot change a Widget's `spec`, create Pods or touch other objects. An operator that creates Pods needs a Role that permits that.

## How a call is identified and checked

1. **The Pod names its ServiceAccount** in `spec.serviceAccountName`.
2. **Kubernetes gives the Pod what it needs to call:** the API server's address in the environment variables `KUBERNETES_SERVICE_HOST` and `KUBERNETES_SERVICE_PORT`, and two files under `/var/run/secrets/kubernetes.io/serviceaccount/`: a token that identifies the ServiceAccount, and a certificate authority bundle to verify the API server's certificate.
3. **The operator sends the token with each call.** The code reads the token file on every call, because Kubernetes replaces it regularly ([api_client.py](../control_loop/python/src/control_loop/kubernetes_operator/api_client.py)).
4. **The API server identifies the caller from the token**, then checks whether role-based access control (RBAC) permits this call for that caller.

These permissions apply to the operator only, not to the person who installs it or to the client who writes Widget specs.

References: [accessing the API from a Pod](https://kubernetes.io/docs/tasks/run-application/access-api-from-pod/), [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/).
