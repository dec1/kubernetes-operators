# Minimal controller: Python

[controller.py](controller.py) is the whole control loop for Widget replicas in one file. It uses only the Python standard library, so there is nothing to install. What it contains and leaves out, compared with the other levels: [Minimal and library controllers](../readme.md).

## Run it

It needs a cluster with the Widget CustomResourceDefinition (CRD) and the sample Widget from [manifests](../../manifests/readme.md) applied. With those, from this directory:

```bash
kubectl proxy &    # Listens on 127.0.0.1:8001 and adds your kubeconfig credentials.
python3 controller.py
```

It watches the namespace `default`; set the environment variable `WATCH_NAMESPACE` for another one. It logs each result:

```text
demo: result {'replicas': 1, 'ready': False}
demo: result {'replicas': 2, 'ready': False}
demo: result {'replicas': 3, 'ready': True}
```

Follow the same result in the cluster with `kubectl get widget demo --watch`. Stop the controller with Ctrl+C.

## Test it

It has no tests; the tested version of this loop is [Structured in Python](../2_structured/python/readme.md). The one check, which CI also runs on every push ([workflow](../../.github/workflows/control-loop.yml)):

```bash
python3 -m py_compile controller.py    # Fails on a syntax error; does not run the controller.
```

The check writes the compiled file to `__pycache__/`, which [.gitignore](.gitignore) keeps out of Git.
