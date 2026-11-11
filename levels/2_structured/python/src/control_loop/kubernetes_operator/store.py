"""Store (operator): the core's WidgetStore for the operator.

Part of our operator, not of Kubernetes: it reads Widget objects that the
API server keeps in etcd, and writes their status through the API server.

    get(name)          reads a local copy (cache) of the Widget object; no API call
    set_result(name)   writes the result into the object's status: PATCH to the API server

The cache is kept current by the Watcher, through replace_all(), put() and
remove(). Reading from it instead of calling the API server on every pass is
what client libraries do too (their "informer cache").

The Watcher and the Worker run in different threads, so every method locks.
"""

import copy
import threading

from control_loop.kubernetes_operator.api_client import KubernetesApi, KubernetesObject, NotFound
from control_loop.core.widget import Result, Widget


class Store:
    """Fits the core's WidgetStore (get, set_result); the web service's
    Store is the same role, in memory."""

    def __init__(self, api: KubernetesApi) -> None:
        self._api = api
        self._objects: dict[str, KubernetesObject] = {}  # name -> latest Widget object seen
        self._lock = threading.Lock()

    # ---- used by the core (WidgetStore)

    def get(self, name: str) -> Widget | None:
        """Spec and result from the cached object; None if absent or being deleted."""
        with self._lock:
            object_ = copy.deepcopy(self._objects.get(name))
        if object_ is None or object_['metadata'].get('deletionTimestamp'):
            return None
        status = object_.get('status') or {}
        result: Result | None = None
        if 'replicas' in status and 'ready' in status:
            result = {'replicas': status['replicas'], 'ready': status['ready']}
        return {'spec': {'replicas': object_['spec']['replicas']}, 'result': result}

    def set_result(self, name: str, result: Result) -> None:
        """PATCH the object's status, unless it already holds this result."""
        with self._lock:
            object_ = self._objects.get(name)
            if object_ is None or object_.get('status') == result:
                return  # Deleted, or nothing new to report: no API call.
            version_before = object_['metadata']['resourceVersion']
        try:
            updated = self._api.patch_status(name, result)
        except NotFound:
            return  # Deleted meanwhile; WidgetStore says: ignore.
        with self._lock:
            # Keep the PATCH response only if the Watcher has not stored a
            # newer object meanwhile. resourceVersions are opaque: compare
            # for equality only, never for "greater than".
            current = self._objects.get(name)
            if current is not None and current['metadata']['resourceVersion'] == version_before:
                self._objects[name] = updated

    # ---- used by the Watcher

    def names(self) -> set[str]:
        with self._lock:
            return set(self._objects)

    def replace_all(self, objects: list[KubernetesObject]) -> None:
        """After a list: the cache becomes exactly these objects."""
        with self._lock:
            self._objects = {object_['metadata']['name']: copy.deepcopy(object_) for object_ in objects}

    def put(self, object_: KubernetesObject) -> None:
        with self._lock:
            self._objects[object_['metadata']['name']] = copy.deepcopy(object_)

    def remove(self, name: str) -> None:
        with self._lock:
            self._objects.pop(name, None)
