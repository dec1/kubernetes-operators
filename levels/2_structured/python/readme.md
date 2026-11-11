# Control loop: Python

This implements the [control loop](../readme.md) described in the parent readme, in Python. It runs as either of the two variants described there: a **web service** with its own HTTP API, or a **Kubernetes operator** that watches Widget objects in a cluster. Both share the same core.

It has the same parts, names and tests as the [Go version](../go/readme.md). The program uses only the standard library (Python 3.12 or newer); the tests use pytest. Each part of the readme's diagrams is one module in the `control_loop` package (`src/control_loop/`):

| module | part | does |
|---|---|---|
| **shared core** | | |
| [widget.py](src/control_loop/core/widget.py) | the boundary | Spec, Result and Widget data; `WidgetStore`: the two methods the core needs from a Store |
| [reconcile.py](src/control_loop/core/reconcile.py) | `reconcile(name)` | compares spec and internal state, changes at most one replica, writes the result |
| [worker.py](src/control_loop/core/worker.py) | Worker | takes a name, calls `reconcile()`, adds the name again if needed |
| [queue.py](src/control_loop/core/queue.py) | Queue | names waiting for a check, without duplicates |
| [internal_state.py](src/control_loop/core/internal_state.py) | Internal state | running replicas per name, in memory |
| **web service** | | |
| [web_service/api.py](src/control_loop/web_service/api.py) | HTTP API | `PUT`, `GET`, `DELETE /widgets/{name}`: reads and writes the Store |
| [web_service/store.py](src/control_loop/web_service/store.py) | Store (web service) | spec and result per name, in memory; a spec write or delete adds the name to the Queue |
| [web_service/main.py](src/control_loop/web_service/main.py) | wiring | starts the HTTP API and the Worker |
| **Kubernetes operator** | | |
| [kubernetes_operator/api_client.py](src/control_loop/kubernetes_operator/api_client.py) | API client | list, watch and status `PATCH` calls to the API server |
| [kubernetes_operator/store.py](src/control_loop/kubernetes_operator/store.py) | Store (operator) | reads a cache of the Widget objects; writes results to their `status` |
| [kubernetes_operator/watcher.py](src/control_loop/kubernetes_operator/watcher.py) | Watcher | keeps the cache current and adds changed names to the Queue |
| [kubernetes_operator/main.py](src/control_loop/kubernetes_operator/main.py) | wiring | starts the Watcher and the Worker |
| [kubernetes_operator/fake_api.py](src/control_loop/kubernetes_operator/fake_api.py) | (for tests) | in-memory stand-in for the API server and etcd |

## Set up

Install [uv](https://docs.astral.sh/uv/getting-started/installation/); nothing else is installed by hand. Every Python command below starts with `uv run`, which runs the command in the project's virtual environment `.venv/`. Before that, `uv run` creates or updates `.venv/` from these files:

| file | uv takes from it |
|---|---|
| [.python-version](.python-version) | the Python version to use: 3.14. uv downloads it if the machine has no Python 3.14 |
| [pyproject.toml](pyproject.toml) | the `control_loop` package, which uv installs as a link to `src/`, so code changes need no reinstall; and the development tool pytest |
| [uv.lock](uv.lock) | the exact version of pytest and of each package pytest needs. uv writes this file; it is committed, so every machine and CI install the same versions |

`uv sync` only creates or updates `.venv/`, without running a command: eg before selecting `.venv/` as the interpreter in an IDE (in PyCharm, also set the default test runner to pytest).

## Run the web service

From this directory:

```bash
uv run python -m control_loop.web_service.main --port 8080
```

`-m` runs the named module of the `control_loop` package.

In a second terminal, set a spec and follow the result:

```bash
curl -X PUT localhost:8080/widgets/demo -d '{"spec": {"replicas": 3}}'
# {"name": "demo", "spec": {"replicas": 3}, "result": null}

curl localhost:8080/widgets/demo
# {"name": "demo", "spec": {"replicas": 3}, "result": {"replicas": 2, "ready": false}}
```

The `PUT` returns at once with `"result": null`; repeat the `GET` to watch `replicas` rise one per second until `"ready": true`. The first terminal logs each result:

```text
demo: result {'replicas': 1, 'ready': False}
demo: result {'replicas': 2, 'ready': False}
demo: result {'replicas': 3, 'ready': True}
```

Lower the spec with another `PUT` and `replicas` falls the same way. `curl -X DELETE localhost:8080/widgets/demo` removes the Widget; the log shows `demo: spec deleted, internal state removed`.

`--help` lists the delays between passes and after a failure.

**Stop it** with Ctrl+C in its terminal (in an IDE: its stop button). `Address already in use` means an earlier run still holds the port: stop that one (`ss -ltnp 'sport = :8080'` shows its process), or choose another port with `--port`.

## Run the Kubernetes operator

Not needed for the tests. It needs a cluster with the Widget CustomResourceDefinition (CRD) and the sample Widget from [manifests](../../../manifests/readme.md) applied. With those, from this directory:

```bash
kubectl proxy &    # Listens on 127.0.0.1:8001 and adds your kubeconfig credentials.
uv run python -m control_loop.kubernetes_operator.main --namespace default
```

Run this way, outside the cluster, the operator acts with your own kubectl permissions. Inside a Pod it uses the Pod's ServiceAccount instead (see [api_client.py](src/control_loop/kubernetes_operator/api_client.py)). Stop it with Ctrl+C.

## Test it

From this directory:

```bash
uv run pytest
```

CI runs the same tests on every push ([workflow](../../../.github/workflows/control-loop.yml)), as `uv run --locked pytest -v`: `--locked` fails if `uv.lock` no longer matches `pyproject.toml`, instead of updating it; `-v` lists each test.

| file in `tests/` | tests | how |
|---|---|---|
| [test_reconcile.py](tests/core/test_reconcile.py) | every row of the readme's [situations table](../readme.md#how-each-situation-is-handled), plus internal state changed by something else | calls `reconcile()` directly; no threads, no waiting |
| [test_queue.py](tests/core/test_queue.py) | order, duplicates, delayed adds | direct calls |
| [test_worker.py](tests/core/test_worker.py) | a failed pass is retried after the retry delay; a finished name is not added again | Worker in a thread with a real Store and Queue; an internal state whose first action fails |
| [web_service/test_store.py](tests/web_service/test_store.py) | which writes add the name to the Queue; results kept and ignored as intended | Store with a list in place of the Queue |
| [web_service/test_smoke.py](tests/web_service/test_smoke.py) | the web service over HTTP: raise, lower, delete, invalid spec | the whole process on a free port, 0.01 s step delay |
| [kubernetes_operator/test_store.py](tests/kubernetes_operator/test_store.py) | cached object to Widget; status `PATCH`, skipped when unchanged; deleted objects ignored | Store (operator) with the fake API |
| [kubernetes_operator/test_watcher.py](tests/kubernetes_operator/test_watcher.py) | list fills the cache; watched changes queue names; after 410 Gone a new list also queues deletions missed meanwhile; with bookmarks no new list is needed | Watcher in a thread with the fake API; no Worker |
| [kubernetes_operator/test_smoke.py](tests/kubernetes_operator/test_smoke.py) | the operator end to end: raise, lower, delete, recovery after 410 Gone | the whole operator with the fake API |

Each test module starts with what it tests and what it leaves to others. [conftest.py](tests/conftest.py) holds `wait_until`, the shared helper for tests that wait on a background thread. Not tested: the real HTTP API client, which needs a cluster.

## Threads

There is one Worker, so `reconcile()` never runs twice at once (why: see [One Worker, one name at a time](../readme.md#actors-and-calls)). A second thread lets the variant's own side keep waiting while the Worker works:

- **Web service**: the HTTP server, which also starts a thread per request; and the Worker.
- **Operator**: the Watcher, which waits on the open watch response; and the Worker.

What they share locks: the Store and the Queue in both variants. The Internal state needs no lock, because only the Worker changes it.
