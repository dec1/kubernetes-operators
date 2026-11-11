"""Unit tests for the Watcher: from Kubernetes changes to cache and Queue.

Tested: the first "LIST" fills the cache and queues every name; "WATCH"
changes (added, modified, deleted) update the cache and queue the name; after
410 Gone the Watcher lists again and also queues names deleted while it could
not watch; with bookmarks it follows the position and needs no new list.

Not tested here: reconcile() and the Worker (no Worker runs; the tests read
the Queue directly), and HTTP (the fake API stands in for the API server).
"""

import threading
from collections.abc import Callable, Iterator

import pytest

from control_loop.kubernetes_operator.fake_api import FakeKubernetesApi
from control_loop.kubernetes_operator.store import Store
from control_loop.kubernetes_operator.watcher import Watcher
from control_loop.core.queue import Queue

WaitUntil = Callable[[Callable[[], bool]], bool]  # The conftest.py fixture.


class Parts:
    """The fake API, and the cache and Queue the Watcher fills."""

    def __init__(self) -> None:
        self.api = FakeKubernetesApi()
        self.store = Store(self.api)
        self.queue = Queue()

    def cached_spec(self, name: str) -> object:
        """The spec in the cache, or None if the name is not cached."""
        widget = self.store.get(name)
        return None if widget is None else widget['spec']

    def queued_names(self) -> set[str]:
        """Take every name waiting in the Queue right now."""
        names = set()
        while (name := self.queue.take(timeout_seconds=0)) is not None:
            names.add(name)
        return names


@pytest.fixture
def parts() -> Parts:
    return Parts()


@pytest.fixture
def start_watcher(parts: Parts) -> Iterator[Callable[[], None]]:
    """Return a function that runs the Watcher in a thread (each watch lasts
    0.05 s); the thread is stopped after the test."""
    stopped = threading.Event()
    thread = threading.Thread(
        target=Watcher(parts.api, parts.store, parts.queue, 0.05, 0.01).run,
        args=(stopped,),
        daemon=True,
    )
    yield thread.start
    stopped.set()
    thread.join()


def test_first_list_fills_the_cache_and_queues_every_name(
    parts: Parts, start_watcher: Callable[[], None], wait_until: WaitUntil
) -> None:
    parts.api.apply('a', 1)
    parts.api.apply('b', 2)
    start_watcher()
    assert wait_until(lambda: parts.store.names() == {'a', 'b'})
    assert parts.queued_names() == {'a', 'b'}


def test_watched_changes_update_the_cache_and_queue_the_name(
    parts: Parts, start_watcher: Callable[[], None], wait_until: WaitUntil
) -> None:
    start_watcher()
    assert wait_until(lambda: parts.api.list_count == 1)

    parts.api.apply('demo', 3)  # ADDED
    assert wait_until(lambda: parts.store.get('demo') is not None)
    assert parts.queued_names() == {'demo'}

    parts.api.apply('demo', 5)  # MODIFIED
    assert wait_until(lambda: parts.cached_spec('demo') == {'replicas': 5})
    assert parts.queued_names() == {'demo'}

    parts.api.delete('demo')  # DELETED
    assert wait_until(lambda: parts.store.get('demo') is None)
    assert parts.queued_names() == {'demo'}


def test_after_410_it_lists_again_and_queues_names_deleted_meanwhile(
    parts: Parts, start_watcher: Callable[[], None], wait_until: WaitUntil
) -> None:
    parts.api.apply('kept', 1)
    parts.api.apply('gone', 1)
    start_watcher()
    assert wait_until(lambda: parts.store.names() == {'kept', 'gone'})
    parts.queued_names()  # Clear the names from the first list.

    # 'gone' is deleted while the Watcher cannot see it: the DELETED change is
    # never delivered, and the Watcher's position has expired.
    parts.api.delete_unseen('gone')

    assert wait_until(lambda: parts.api.list_count == 2)  # Listed again after 410.
    assert wait_until(lambda: parts.store.names() == {'kept'})
    # 'gone' is queued although no DELETED change arrived: reconcile() will
    # find no object and remove its internal state (the removed group).
    assert 'gone' in parts.queued_names()


def test_with_bookmarks_no_new_list_is_needed(
    parts: Parts, start_watcher: Callable[[], None], wait_until: WaitUntil
) -> None:
    start_watcher()
    assert wait_until(lambda: parts.api.list_count == 1)

    # Other cluster writes move the position; the Watcher learns the new
    # position from the bookmark and watches from it next. Then the history
    # before that position is dropped.
    parts.api.send_bookmark()
    position = parts.api.position()
    assert wait_until(lambda: parts.api.watched_from == position)
    parts.api.forget_history()

    parts.api.apply('demo', 1)  # Arrives through the same, still valid, position.
    assert wait_until(lambda: parts.store.get('demo') is not None)
    assert parts.api.list_count == 1  # No 410, so no second list.
