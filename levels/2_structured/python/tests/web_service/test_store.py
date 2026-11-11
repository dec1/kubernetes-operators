"""Unit tests for the Store: what it keeps and when it notifies.

Tested: spec writes and deletes call notify (so the name reaches the Queue);
result writes do not; a new spec keeps the old result; a result for a deleted
name is dropped; get() returns copies.

Not tested here: the lock under concurrent access (the smoke test runs the
Store from two threads, but does not try to provoke races), and the Queue
itself: a list stands in for it and records the notified names.
"""

import pytest

from control_loop.web_service.store import Store


@pytest.fixture
def notified() -> list[str]:
    return []  # Names the Store passed to notify, in order.


@pytest.fixture
def store(notified: list[str]) -> Store:
    return Store(notify=notified.append)


def test_writing_a_spec_notifies_with_the_name(store: Store, notified: list[str]) -> None:
    store.put_spec('demo', {'replicas': 3})
    assert notified == ['demo']
    assert store.get('demo') == {'spec': {'replicas': 3}, 'result': None}


def test_writing_a_result_does_not_notify(store: Store, notified: list[str]) -> None:
    store.put_spec('demo', {'replicas': 3})
    store.set_result('demo', {'replicas': 1, 'ready': False})
    assert notified == ['demo']  # Only the spec write.


def test_new_spec_keeps_the_previous_result_until_reconcile_writes_one(store: Store) -> None:
    store.put_spec('demo', {'replicas': 3})
    store.set_result('demo', {'replicas': 3, 'ready': True})
    store.put_spec('demo', {'replicas': 5})
    widget = store.get('demo')
    assert widget is not None
    assert widget['result'] == {'replicas': 3, 'ready': True}


def test_deleting_notifies_and_removes(store: Store, notified: list[str]) -> None:
    store.put_spec('demo', {'replicas': 3})
    assert store.delete('demo') is True
    assert notified == ['demo', 'demo']
    assert store.get('demo') is None


def test_deleting_an_absent_name_does_not_notify(store: Store, notified: list[str]) -> None:
    assert store.delete('demo') is False
    assert notified == []


def test_result_for_a_deleted_name_is_ignored(store: Store) -> None:
    store.set_result('demo', {'replicas': 0, 'ready': True})
    assert store.get('demo') is None


def test_get_returns_a_copy(store: Store) -> None:
    store.put_spec('demo', {'replicas': 3})
    copy = store.get('demo')
    assert copy is not None
    copy['spec']['replicas'] = 99
    widget = store.get('demo')
    assert widget is not None
    assert widget['spec']['replicas'] == 3
