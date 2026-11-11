"""Unit tests for the Worker: how it schedules the next pass.

Tested: after a failed pass the name is added again after retry_delay (not
step_delay), and the next pass continues; once the spec is met, the name is not
added again.

Not tested here: what reconcile() decides (see test_reconcile.py) and HTTP
(see test_smoke.py). The Worker runs in a thread, with a real Store and Queue
and no HTTP API.
"""

import threading
from collections.abc import Callable, Iterator

import pytest

from control_loop.core.internal_state import InternalState
from control_loop.core.queue import Queue
from control_loop.web_service.store import Store
from control_loop.core.worker import run_worker

WaitUntil = Callable[[Callable[[], bool]], bool]  # The conftest.py fixture.


class FailOnceInternalState(InternalState):
    """Internal state whose first add_replica() fails; later ones succeed."""

    def __init__(self) -> None:
        super().__init__()
        self.failures = 0

    def add_replica(self, name: str) -> None:
        if self.failures == 0:
            self.failures += 1
            raise RuntimeError('cannot start replica')
        super().add_replica(name)


class Parts:
    """The parts the Worker uses, created fresh for each test."""

    def __init__(self, internal_state: InternalState) -> None:
        self.queue = Queue()
        self.store = Store(notify=self.queue.add)
        self.internal_state = internal_state


StartWorker = Callable[[InternalState, float, float], Parts]


@pytest.fixture
def start_worker() -> Iterator[StartWorker]:
    """Return a function that runs the Worker in a thread with the given
    internal state, step_delay and retry_delay; the thread is stopped after
    the test."""
    stopped = threading.Event()
    threads: list[threading.Thread] = []

    def start(internal_state: InternalState, step_delay: float, retry_delay: float) -> Parts:
        parts = Parts(internal_state)
        thread = threading.Thread(
            target=run_worker,
            args=(parts.queue, parts.store, internal_state, step_delay, retry_delay, stopped),
            daemon=True,
        )
        thread.start()
        threads.append(thread)
        return parts

    yield start
    stopped.set()
    for thread in threads:
        thread.join()


def test_failed_pass_is_retried_after_retry_delay(
    start_worker: StartWorker, wait_until: WaitUntil
) -> None:
    # step_delay 10 s, retry_delay 0.01 s: finishing within wait_until's 5 s
    # shows the Worker waited retry_delay after the failure, not step_delay.
    internal_state = FailOnceInternalState()
    parts = start_worker(internal_state, 10.0, 0.01)
    parts.store.put_spec('demo', {'replicas': 1})  # One step, after the failure.
    assert wait_until(lambda: internal_state.replicas('demo') == 1)
    assert internal_state.failures == 1


def test_name_is_not_added_again_once_the_spec_is_met(
    start_worker: StartWorker, wait_until: WaitUntil
) -> None:
    # step_delay 0.01 s: a wrong "check again" would arrive well within the
    # 0.2 s that take() waits below.
    internal_state = InternalState()
    internal_state.add_replica('demo')  # Already meets the spec below.
    parts = start_worker(internal_state, 0.01, 0.01)
    parts.store.put_spec('demo', {'replicas': 1})

    def ready() -> bool:
        widget = parts.store.get('demo')
        return widget is not None and widget['result'] == {'replicas': 1, 'ready': True}

    assert wait_until(ready)
    assert parts.queue.take(timeout_seconds=0.2) is None  # Nothing scheduled.
