"""Kubernetes operator: the control loop with Kubernetes storing the specs.

Run (against a cluster, outside it):
    kubectl proxy &
    PYTHONPATH=src python3 -m control_loop.kubernetes_operator.main --namespace default
Then:  kubectl apply -f a Widget object with spec.replicas: 3
       kubectl get widget demo -o yaml      # status shows the result
This needs a cluster with the Widget CRD installed; the tests need neither.

Wiring: a spec written with kubectl is stored by the API server; the Watcher
sees the change, updates the Store's cache and adds the name to the
Queue; the Worker takes it and calls reconcile(), the same as in the web
service. Compared with web_service/main.py, only the Store and the source of
names differ.
"""

import argparse
import threading

from control_loop.core.internal_state import InternalState
from control_loop.kubernetes_operator.api_client import HttpKubernetesApi, KubernetesApi
from control_loop.kubernetes_operator.store import Store
from control_loop.kubernetes_operator.watcher import Watcher
from control_loop.core.queue import Queue
from control_loop.core.worker import run_worker


class KubernetesOperator:
    """Creates and connects the parts; start() runs them in threads.

    Takes any KubernetesApi: the real HttpKubernetesApi in main(), the
    in-memory fake in the tests.
    """

    def __init__(
        self,
        api: KubernetesApi,
        step_delay: float,
        retry_delay: float,
        watch_timeout_seconds: float,
    ) -> None:
        self.queue = Queue()
        self.store = Store(api)
        self.internal_state = InternalState()
        watcher = Watcher(api, self.store, self.queue, watch_timeout_seconds, retry_delay)
        self._stopped = threading.Event()
        # Daemon threads: they do not keep the process alive after main() ends.
        self._threads = [
            threading.Thread(target=watcher.run, args=(self._stopped,), daemon=True),
            threading.Thread(
                target=run_worker,
                args=(
                    self.queue,
                    self.store,
                    self.internal_state,
                    step_delay,
                    retry_delay,
                    self._stopped,
                ),
                daemon=True,
            ),
        ]

    def start(self) -> None:
        for thread in self._threads:
            thread.start()

    def stop(self) -> None:
        """Stop both threads and wait until they have ended. The Watcher ends
        when its current watch response ends: up to watch_timeout_seconds."""
        self._stopped.set()
        for thread in self._threads:
            thread.join()


def main() -> None:
    parser = argparse.ArgumentParser(
        description='Control loop for Widget replicas: Kubernetes operator.'
    )
    parser.add_argument('--namespace', default='default', help='namespace to watch')
    parser.add_argument(
        '--step-delay',
        type=float,
        default=1.0,
        help='seconds between passes while the spec is not met',
    )
    parser.add_argument(
        '--retry-delay',
        type=float,
        default=2.0,
        help='seconds before retrying a failed pass or watch',
    )
    args = parser.parse_args()

    api = HttpKubernetesApi.from_environment(args.namespace)
    operator = KubernetesOperator(api, args.step_delay, args.retry_delay, watch_timeout_seconds=300)
    operator.start()
    print(f'watching Widgets in namespace {args.namespace!r}', flush=True)
    try:
        threading.Event().wait()  # Run until Ctrl+C.
    except KeyboardInterrupt:
        pass  # Exit without stop(): it would wait for the open watch to end.


if __name__ == '__main__':
    main()
