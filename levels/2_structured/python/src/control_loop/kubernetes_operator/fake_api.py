"""FakeKubernetesApi: an in-memory stand-in for the API server and its etcd.

Used by the tests, so they need no cluster. It behaves like the real server in
the ways the operator depends on:

    - objects and a change history, each change at a new position (resourceVersion);
    - list returns all objects and the current position;
    - watch replays the changes after a position, then waits for new ones until
      its timeout; positions older than the kept history raise ResourceExpired;
    - a status PATCH changes the object (a new position) unless the status is
      already equal, in which case nothing changes, as on the real server.

The tests play the Client with apply() and delete() (what kubectl would do),
and use forget_history(), delete_unseen() and send_bookmark() to trigger
410 Gone, a missed deletion and BOOKMARK.

Not modelled: validation against a CRD schema, namespaces, deletionTimestamp
and finalizers, authentication.
"""

import copy
import threading
import time
from collections.abc import Iterator

from control_loop.kubernetes_operator.api_client import (
    KubernetesObject,
    NotFound,
    ResourceExpired,
    WatchEvent,
)
from control_loop.core.widget import Result


class FakeKubernetesApi:
    """Fits KubernetesApi (list_widgets, watch_widgets, patch_status)."""

    def __init__(self) -> None:
        self._objects: dict[str, KubernetesObject] = {}
        self._history: list[tuple[int, WatchEvent]] = []  # (position, change), oldest first
        self._position = 0  # Position of the latest change.
        self._oldest_kept = 0  # Watches must start at or after this position.
        self._changed = threading.Condition()
        self.patch_count = 0  # Status PATCHes received, including unchanged ones.
        self.list_count = 0  # Lists served: shows whether a watcher had to list again.
        self.watched_from: str | None = None  # Position of the latest watch request.

    # ---- the Client's side (what kubectl would do)

    def apply(self, name: str, replicas: int) -> None:
        """Create the Widget, or change its spec."""
        with self._changed:
            object_ = self._objects.get(name)
            if object_ is None:
                object_ = {
                    'apiVersion': 'example.com/v1',
                    'kind': 'Widget',
                    'metadata': {'name': name},
                    'spec': {'replicas': replicas},
                }
                self._objects[name] = object_
                self._record('ADDED', object_)
            else:
                object_['spec'] = {'replicas': replicas}
                self._record('MODIFIED', object_)

    def delete(self, name: str) -> None:
        with self._changed:
            object_ = self._objects.pop(name)
            self._record('DELETED', object_)

    def delete_unseen(self, name: str) -> None:
        """Delete the object and drop the history in one step: watchers never
        receive the DELETED change, as if they were disconnected meanwhile."""
        with self._changed:
            object_ = self._objects.pop(name)
            self._record('DELETED', object_)
            self._history.clear()
            self._oldest_kept = self._position

    def forget_history(self) -> None:
        """Drop the change history, as the real server does after a while:
        watches from any earlier position now fail with 410 Gone."""
        with self._changed:
            self._history.clear()
            self._oldest_kept = self._position

    def send_bookmark(self) -> None:
        """Other objects in the cluster changed, moving the position, but no
        Widget did. Watchers receive only a BOOKMARK with the new position;
        without it, their position would fall behind and later expire."""
        with self._changed:
            self._position += 1
            bookmark: KubernetesObject = {
                'kind': 'Widget',
                'metadata': {'resourceVersion': str(self._position)},
            }
            self._history.append((self._position, {'type': 'BOOKMARK', 'object': bookmark}))
            self._changed.notify_all()

    def position(self) -> str:
        """The current position (resourceVersion), without counting as a list."""
        with self._changed:
            return str(self._position)

    def object(self, name: str) -> KubernetesObject | None:
        """The stored object, as `kubectl get widget <name> -o yaml` would show it."""
        with self._changed:
            return copy.deepcopy(self._objects.get(name))

    # ---- KubernetesApi: the operator's side

    def list_widgets(self) -> tuple[list[KubernetesObject], str]:
        with self._changed:
            self.list_count += 1
            return copy.deepcopy(list(self._objects.values())), str(self._position)

    def watch_widgets(self, resource_version: str, timeout_seconds: float) -> Iterator[WatchEvent]:
        self.watched_from = resource_version
        position = int(resource_version)
        if position < self._oldest_kept:
            raise ResourceExpired(f'position {position} is older than the kept history')
        deadline = time.monotonic() + timeout_seconds
        while True:
            with self._changed:
                self._changed.wait_for(
                    lambda: self._history and self._history[-1][0] > position
                    or time.monotonic() >= deadline,
                    timeout=max(0.0, deadline - time.monotonic()),
                )
                new = [(p, copy.deepcopy(e)) for p, e in self._history if p > position]
            if not new and time.monotonic() >= deadline:
                return  # Timeout: the server ends the response.
            for change_position, event in new:  # Yield outside the lock.
                position = change_position
                yield event

    def patch_status(self, name: str, status: Result) -> KubernetesObject:
        with self._changed:
            self.patch_count += 1
            object_ = self._objects.get(name)
            if object_ is None:
                raise NotFound(name)
            if object_.get('status') != status:
                object_['status'] = dict(status)
                self._record('MODIFIED', object_)
            return copy.deepcopy(object_)

    # ---- internal

    def _record(self, change: str, object_: KubernetesObject) -> None:
        """Give the object a new position and append the change to the history.
        Called with the lock held."""
        self._position += 1
        object_['metadata']['resourceVersion'] = str(self._position)
        self._history.append((self._position, {'type': change, 'object': copy.deepcopy(object_)}))
        self._changed.notify_all()
