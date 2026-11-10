# Library controller: Go

[controller.go](controller.go) is the control loop for Widget replicas on controller-runtime, the Go library Kubernetes operators are usually built on. Our code is only `Reconcile` and the wiring. What the library supplies, compared with the other levels: [Minimal and library controllers](../../readme.md).

## Run it

It needs a cluster with the Widget CustomResourceDefinition (CRD) and the sample Widget from [manifests](../../../manifests/readme.md) applied. The library reads `~/.kube/config` itself, so no `kubectl proxy` is needed. From this directory:

```bash
go run .    # The first run downloads controller-runtime, in the version recorded in go.mod and go.sum.
```

It watches the namespace `default`; set the environment variable `WATCH_NAMESPACE` for another one. It logs each result as one JSON line. Follow the same result in the cluster with `kubectl get widget demo --watch`. Stop the controller with Ctrl+C.

## Test it

It has no tests; the tested version of this loop is [control_loop/go](../../../control_loop/go/readme.md). The checks, which CI also runs on every push ([workflow](../../../.github/workflows/control-loop.yml)):

```bash
test -z "$(gofmt -l .)" # Fails if a file is not gofmt-formatted; `gofmt -l .` lists those files.
go vet .                # Compiles the controller and reports suspicious code; does not run it.
```
