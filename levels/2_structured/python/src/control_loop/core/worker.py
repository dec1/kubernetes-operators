"""Worker: takes names from the Queue and calls reconcile() for each.

Shared by both variants. reconcile() decides; the Worker only schedules the
next pass: "not finished" -> again after step_delay, "failed" -> again after
retry_delay, "done" -> nothing.
"""

import threading

from control_loop.core.internal_state import InternalState
from control_loop.core.queue import Queue
from control_loop.core.reconcile import reconcile
from control_loop.core.widget import WidgetStore


def run_worker(
    queue: Queue,
    store: WidgetStore,
    internal_state: InternalState,
    step_delay: float,
    retry_delay: float,
    stopped: threading.Event,
) -> None:
    """Run until stopped is set; meant for its own thread."""
    while not stopped.is_set():
        name = queue.take(timeout_seconds=0.1)  # Short wait, to notice stopped.
        if name is None:
            continue
        try:
            needs_another_pass = reconcile(name, store, internal_state)
        except Exception as error:
            # The action failed; the next pass compares again from scratch.
            print(f'{name}: reconcile failed, retrying in {retry_delay} s: {error}', flush=True)
            queue.add_after(name, retry_delay)
            continue
        widget = store.get(name)
        if widget is None:
            print(f'{name}: spec deleted, internal state removed', flush=True)
        else:
            print(f'{name}: result {widget["result"]}', flush=True)
        if needs_another_pass:
            queue.add_after(name, step_delay)
