"""Unit tests for Store: reading from the cache, writing status.

Tested: get() turns a cached object into the core's Widget, and returns None
for an absent object or one being deleted; set_result() PATCHes the status,
skips the PATCH when the status already holds the result, and ignores an
object deleted meanwhile.

Not tested here: how the cache is kept current (see test_watcher.py), and
HTTP (the fake API stands in for the API server).
"""

import pytest

from control_loop.kubernetes_operator.fake_api import FakeKubernetesApi
from control_loop.kubernetes_operator.store import Store


@pytest.fixture
def api() -> FakeKubernetesApi:
    api = FakeKubernetesApi()
    api.apply('demo', 3)
    return api


@pytest.fixture
def store(api: FakeKubernetesApi) -> Store:
    """A store whose cache holds the API's current objects, as after a list."""
    store = Store(api)
    objects, _ = api.list_widgets()
    store.replace_all(objects)
    return store


def test_get_returns_spec_and_no_result_before_the_first_status(store: Store) -> None:
    assert store.get('demo') == {'spec': {'replicas': 3}, 'result': None}


def test_get_returns_none_for_an_absent_object(store: Store) -> None:
    assert store.get('other') is None


def test_get_returns_none_for_an_object_being_deleted(
    api: FakeKubernetesApi, store: Store
) -> None:
    # The API server sets deletionTimestamp when deletion is requested but
    # cannot finish yet, eg while a finalizer is pending.
    object_ = api.object('demo')
    assert object_ is not None
    object_['metadata']['deletionTimestamp'] = '2026-11-04T12:00:00Z'
    store.put(object_)
    assert store.get('demo') is None


def test_set_result_patches_the_status(api: FakeKubernetesApi, store: Store) -> None:
    store.set_result('demo', {'replicas': 1, 'ready': False})
    stored = api.object('demo')
    assert stored is not None
    assert stored['status'] == {'replicas': 1, 'ready': False}
    assert store.get('demo') == {
        'spec': {'replicas': 3},
        'result': {'replicas': 1, 'ready': False},
    }


def test_set_result_skips_the_patch_when_the_status_is_unchanged(
    api: FakeKubernetesApi, store: Store
) -> None:
    store.set_result('demo', {'replicas': 1, 'ready': False})
    store.set_result('demo', {'replicas': 1, 'ready': False})
    assert api.patch_count == 1


def test_set_result_for_an_object_deleted_meanwhile_is_ignored(
    api: FakeKubernetesApi, store: Store
) -> None:
    api.delete('demo')  # Deleted in Kubernetes; the cache has not seen it yet.
    store.set_result('demo', {'replicas': 1, 'ready': False})  # Must not raise.

