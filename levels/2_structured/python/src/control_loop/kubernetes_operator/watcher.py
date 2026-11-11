"""Watcher (operator): turns Kubernetes changes into names for the Queue.

It replaces the web service's Store.notify. Each change updates the
Store's cache and adds the object's name to the Queue:

    1. "LIST": all Widget objects and the current position (resourceVersion).
       The cache becomes exactly these objects. Every name listed, plus every
       name cached before but no longer listed (deleted while not watching),
       goes to the Queue: the added, remained and removed groups.
    2. "WATCH" from that position: each ADDED, MODIFIED or DELETED change
       updates the cache and queues the name; BOOKMARK only moves the position.
       When the server ends the response (timeout), watch again from the
       latest position: no new list needed.
    3. 410 Gone (ResourceExpired): the position is too old; back to 1.
       Any other error: wait, then watch again from the same position.
"""

import threading

from control_loop.kubernetes_operator.api_client import KubernetesApi, ResourceExpired
from control_loop.kubernetes_operator.store import Store
from control_loop.core.queue import Queue


class Watcher:
    def __init__(
        self,
        api: KubernetesApi,
        store: Store,
        queue: Queue,
        watch_timeout_seconds: float,
        retry_delay: float,
    ) -> None:
        self._api = api
        self._store = store
        self._queue = queue
        self._watch_timeout = watch_timeout_seconds
        self._retry_delay = retry_delay

    def run(self, stopped: threading.Event) -> None:
        """Run until stopped is set; meant for its own thread."""
        resource_version: str | None = None  # None: a list is needed first.
        while not stopped.is_set():
            try:
                if resource_version is None:
                    resource_version = self._list()
                resource_version = self._watch(resource_version, stopped)
            except ResourceExpired:
                print('watch position expired (410 Gone): listing again', flush=True)
                resource_version = None
            except Exception as error:
                print(f'watch failed, retrying in {self._retry_delay} s: {error}', flush=True)
                stopped.wait(self._retry_delay)

    def _list(self) -> str:
        """Step 1: refill the cache and queue every name that may have changed."""
        objects, resource_version = self._api.list_widgets()
        cached_before = self._store.names()
        self._store.replace_all(objects)
        for name in cached_before | self._store.names():
            self._queue.add(name)
        return resource_version

    def _watch(self, resource_version: str, stopped: threading.Event) -> str:
        """Step 2: apply each change; return the latest position."""
        for event in self._api.watch_widgets(resource_version, self._watch_timeout):
            object_ = event['object']
            resource_version = object_['metadata']['resourceVersion']
            if event['type'] == 'BOOKMARK':
                continue  # No object changed; only the position moved.
            name = object_['metadata']['name']
            if event['type'] == 'DELETED':
                self._store.remove(name)
            else:
                self._store.put(object_)
            self._queue.add(name)
            if stopped.is_set():
                break
        return resource_version
