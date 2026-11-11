# Control loop: Go

This implements the [control loop](../readme.md) described in the parent readme, in Go. It runs as either of the two variants described there: a **web service** with its own HTTP API, or a **Kubernetes operator** that watches Widget objects in a cluster. Both share the same core.

It has the same parts, names and tests as the [Python version](../python/readme.md), and uses only the standard library (Go 1.24 or newer), so there is nothing to download.

| file | part | does |
|---|---|---|
| **shared core**: `internal/core/` | | |
| [widget.go](internal/core/widget.go) | the boundary | Spec, Result and Widget data; `WidgetStore`: the two methods the core needs from a Store |
| [reconcile.go](internal/core/reconcile.go) | `Reconcile(name)` | compares spec and internal state, changes at most one replica, writes the result |
| [worker.go](internal/core/worker.go) | Worker | takes a name, calls `Reconcile`, adds the name again if needed |
| [queue.go](internal/core/queue.go) | Queue | names waiting for a check, without duplicates |
| [internalstate.go](internal/core/internalstate.go) | Internal state | running replicas per name, in memory |
| **web service**: `internal/webservice/` | | |
| [api.go](internal/webservice/api.go) | HTTP API | `PUT`, `GET`, `DELETE /widgets/{name}`: reads and writes the Store |
| [store.go](internal/webservice/store.go) | Store (web service) | spec and result per name, in memory; a spec write or delete adds the name to the Queue |
| [webservice.go](internal/webservice/webservice.go) | wiring | starts the HTTP API and the Worker |
| **Kubernetes operator**: `internal/kubernetesoperator/` | | |
| [apiclient.go](internal/kubernetesoperator/apiclient.go) | API client | list, watch and status `PATCH` calls to the API server |
| [store.go](internal/kubernetesoperator/store.go) | Store (operator) | reads a cache of the Widget objects; writes results to their `status` |
| [watcher.go](internal/kubernetesoperator/watcher.go) | Watcher | keeps the cache current and adds changed names to the Queue |
| [operator.go](internal/kubernetesoperator/operator.go) | wiring | starts the Watcher and the Worker |
| **programs**: `cmd/` | | |
| [webservice/main.go](cmd/webservice/main.go) | | runs the web service |
| [kubernetesoperator/main.go](cmd/kubernetesoperator/main.go) | | runs the operator |

`internal/` means only this module may import these packages. Each `cmd/` program only reads its flags and calls the wiring.

## Differences from Python

Go idioms, the same design:

| | Python | Go |
|---|---|---|
| stopping goroutines/threads | `threading.Event` | `context.Context`, cancelled by `Stop` |
| a failed action | raises an exception | returns an `error` |
| Internal state | a class; tests subclass it to make an action fail | an interface; tests embed it and replace one method |
| Internal state lock | none: only the Worker changes it | a mutex: tests read it meanwhile, and `go test -race` reports unlocked access |
| watch stream | a generator | an iterator (`iter.Seq2`, range-over-func) |
| fake API server | `kubernetes_operator/fake_api.py`, in the package | `fakeapi_test.go`: in a `_test.go` file, Go compiles it only for the tests |
| shared test helper | `tests/conftest.py`: `wait_until` | `internal/testutil`: `WaitUntil` |
| delay flags | seconds: `--step-delay 1.0` | durations: `-step-delay 1s` |

## Run the web service

From this directory:

```bash
go run ./cmd/webservice -port 8080
```

In a second terminal, set a spec and follow the result:

```bash
curl -X PUT localhost:8080/widgets/demo -d '{"spec": {"replicas": 3}}'
# {"name":"demo","spec":{"replicas":3},"result":null}

curl localhost:8080/widgets/demo
# {"name":"demo","spec":{"replicas":3},"result":{"replicas":2,"ready":false}}
```

The first terminal logs each result, eg `demo: result {"replicas":1,"ready":false}`. Lowering the spec and `DELETE` work as in the Python version. `-h` lists the delays.

**Stop it** with Ctrl+C in its terminal (in an IDE: its stop button). `address already in use` means an earlier run still holds the port: stop that one (`ss -ltnp 'sport = :8080'` shows its process), or choose another port with `-port`.

## Run the Kubernetes operator

Not needed for the tests. It needs a cluster with the Widget CustomResourceDefinition (CRD) and the sample Widget from [manifests](../../../manifests/readme.md) applied. With those, from this directory:

```bash
kubectl proxy &    # Listens on 127.0.0.1:8001 and adds your kubeconfig credentials.
go run ./cmd/kubernetesoperator -namespace default
```

Run this way, outside the cluster, the operator acts with your own kubectl permissions. Inside a Pod it uses the Pod's ServiceAccount instead (see [apiclient.go](internal/kubernetesoperator/apiclient.go)). Stop it with Ctrl+C.

## Test it

From this directory; CI runs the same commands on every push ([workflow](../../../.github/workflows/control-loop.yml)):

```bash
test -z "$(gofmt -l .)" # Fails if a file is not gofmt-formatted; `gofmt -l .` lists those files.
go vet ./...
go test -race ./...     # -race: report unsynchronized access between goroutines
```

The tests mirror the Python ones, one file each, with the same "Tested" and "Not tested here" comment at the top:

| file | tests |
|---|---|
| [core/reconcile_test.go](internal/core/reconcile_test.go) | every row of the readme's [situations table](../readme.md#how-each-situation-is-handled), plus internal state changed by something else |
| [core/queue_test.go](internal/core/queue_test.go) | order, duplicates, delayed adds |
| [core/worker_test.go](internal/core/worker_test.go) | a failed pass is retried after the retry delay; a finished name is not added again |
| [webservice/store_test.go](internal/webservice/store_test.go) | which writes add the name to the Queue; results kept and ignored as intended |
| [webservice/smoke_test.go](internal/webservice/smoke_test.go) | the web service over HTTP: raise, lower, delete, invalid spec |
| [kubernetesoperator/store_test.go](internal/kubernetesoperator/store_test.go) | cached object to Widget; status `PATCH`, skipped when unchanged; deleted objects ignored |
| [kubernetesoperator/watcher_test.go](internal/kubernetesoperator/watcher_test.go) | list fills the cache; watched changes queue names; after 410 Gone a new list also queues deletions missed meanwhile; with bookmarks no new list is needed |
| [kubernetesoperator/smoke_test.go](internal/kubernetesoperator/smoke_test.go) | the operator end to end with the fake API: raise, lower, delete, recovery after 410 Gone |

Not tested: the real HTTP API client, which needs a cluster.

## Goroutines

There is one Worker, so `Reconcile` never runs twice at once (why: see [One Worker, one name at a time](../readme.md#actors-and-calls)). A second goroutine lets the variant's own side keep waiting while the Worker works:

- **Web service**: the HTTP server, which also starts a goroutine per request; and the Worker.
- **Operator**: the Watcher, which waits on the open watch response; and the Worker.

What they share locks: the Store and the Queue in both variants. The Internal state locks too, although only the Worker changes it, because the tests read it meanwhile and `go test -race` would report that.
