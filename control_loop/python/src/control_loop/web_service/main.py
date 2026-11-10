"""Web service: the control loop with its own HTTP API and in-memory Store.

Run:   PYTHONPATH=src python3 -m control_loop.web_service.main --port 8080
Then:  curl -X PUT localhost:8080/widgets/demo -d '{"spec": {"replicas": 3}}'
       curl localhost:8080/widgets/demo

Wiring: a spec written through the HTTP API goes into the Store, whose notify
adds the name to the Queue; the Worker takes it and calls reconcile().

Threads: the HTTP server handles each request in its own thread; one Worker
thread takes names from the Queue. Specs and results cross between them only
through the Store, which locks.
"""

import argparse
import http.server
import threading

from control_loop.core.internal_state import InternalState
from control_loop.core.queue import Queue
from control_loop.web_service.api import make_handler
from control_loop.web_service.store import Store
from control_loop.core.worker import run_worker


class WebService:
    """Creates and connects the parts; start() runs them in threads.

    Used by main() and by the smoke test. port=0 lets the system choose a free
    port; self.port is the one actually used.
    """

    def __init__(self, port: int, step_delay: float, retry_delay: float) -> None:
        self.queue = Queue()
        self.store = Store(notify=self.queue.add)  # Spec changes add the name.
        self.internal_state = InternalState()
        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', port), make_handler(self.store))
        self.port = self.server.server_address[1]  # The actual port, also when 0 was given.
        self._stopped = threading.Event()
        # Daemon threads: they do not keep the process alive after main() ends.
        self._threads = [
            threading.Thread(target=self.server.serve_forever, daemon=True),
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
        """Stop both threads and wait until they have ended."""
        self._stopped.set()
        self.server.shutdown()  # Stops serve_forever().
        self.server.server_close()  # Releases the port.
        for thread in self._threads:
            thread.join()


def main() -> None:
    parser = argparse.ArgumentParser(description='Control loop for Widget replicas: web service.')
    parser.add_argument('--port', type=int, default=8080)
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
        help='seconds before retrying a failed pass',
    )
    args = parser.parse_args()

    service = WebService(args.port, args.step_delay, args.retry_delay)
    service.start()
    print(f'listening on http://127.0.0.1:{service.port}', flush=True)
    try:
        threading.Event().wait()  # Run until Ctrl+C.
    except KeyboardInterrupt:
        service.stop()


if __name__ == '__main__':
    main()
