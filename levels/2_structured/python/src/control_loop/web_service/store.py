"""Store (web service): the spec and latest result of each Widget, in memory.

Kubernetes counterpart: etcd. Written by the HTTP API (spec) and by
reconcile() (result). Writing or deleting a spec calls notify(name), which
adds the name to the Queue. Writing a result does not: a result is a report,
and checking again because of it would only find nothing to change.

The HTTP API and the Worker run in different threads, so every method takes
a lock.
"""

import copy
import threading
from collections.abc import Callable

from control_loop.core.widget import Result, Spec, Widget


class Store:
    """Specs and results by name, in memory; thread-safe.
    Fits the core's WidgetStore (get, set_result); put_spec and delete are
    used by the HTTP API."""

    def __init__(self, notify: Callable[[str], None]) -> None:
        self._notify = notify  # Called with a name after its spec changes.
        self._widgets: dict[str, Widget] = {}
        self._lock = threading.Lock()

    def put_spec(self, name: str, spec: Spec) -> None:
        """Create the Widget or replace its spec. The previous result is kept
        until reconcile() writes a new one, so a result can be out of date."""
        with self._lock:
            widget = self._widgets.get(name)
            if widget is None:
                self._widgets[name] = {'spec': copy.deepcopy(spec), 'result': None}
            else:
                widget['spec'] = copy.deepcopy(spec)
        self._notify(name)

    def get(self, name: str) -> Widget | None:
        """Return a copy of the Widget, or None if absent.
        A copy, so changing it does not change the Store."""
        with self._lock:
            return copy.deepcopy(self._widgets.get(name))

    def delete(self, name: str) -> bool:
        """Remove the Widget. Returns False if it did not exist."""
        with self._lock:
            existed = self._widgets.pop(name, None) is not None
        if existed:
            self._notify(name)  # reconcile() then removes the internal state.
        return existed

    def set_result(self, name: str, result: Result) -> None:
        """Write the result. Ignored if the Widget was deleted meanwhile."""
        with self._lock:
            widget = self._widgets.get(name)
            if widget is not None:
                widget['result'] = copy.deepcopy(result)
