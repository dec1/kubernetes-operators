"""Smoke test: run the whole operator against the fake API, as kubectl would use it.

Tested: the parts are wired together correctly. A Widget applied to the API
server reaches its spec one replica at a time, with the result written to the
object's status; a lowered spec is reached too; deleting the object removes
its internal state; after 410 Gone the operator recovers on its own.

Not tested here: each part in detail (see the other tests), the real HTTP
client (needs a cluster), and behaviour under load.
"""

from collections.abc import Callable, Iterator
from typing import Any

import pytest

from control_loop.kubernetes_operator.fake_api import FakeKubernetesApi
from control_loop.kubernetes_operator.main import KubernetesOperator

WaitUntil = Callable[[Callable[[], bool]], bool]  # The conftest.py fixture.


@pytest.fixture
def api() -> FakeKubernetesApi:
    return FakeKubernetesApi()


@pytest.fixture
def operator(api: FakeKubernetesApi) -> Iterator[KubernetesOperator]:
    """A running operator for one test; stopped afterwards."""
    operator = KubernetesOperator(api, step_delay=0.01, retry_delay=0.01, watch_timeout_seconds=0.05)
    operator.start()
    yield operator
    operator.stop()


def status(api: FakeKubernetesApi, name: str) -> Any:
    """The object's status, as `kubectl get widget <name> -o yaml` would show it."""
    object_ = api.object(name)
    return None if object_ is None else object_.get('status')


def test_spec_changes_are_reached_and_deletion_cleans_up(
    api: FakeKubernetesApi, operator: KubernetesOperator, wait_until: WaitUntil
) -> None:
    api.apply('demo', 3)
    assert wait_until(lambda: status(api, 'demo') == {'replicas': 3, 'ready': True})

    api.apply('demo', 1)
    assert wait_until(lambda: status(api, 'demo') == {'replicas': 1, 'ready': True})

    api.delete('demo')
    assert wait_until(lambda: operator.internal_state.replicas('demo') == 0)


def test_recovers_after_410_and_cleans_up_a_missed_deletion(
    api: FakeKubernetesApi, operator: KubernetesOperator, wait_until: WaitUntil
) -> None:
    api.apply('demo', 2)
    assert wait_until(lambda: status(api, 'demo') == {'replicas': 2, 'ready': True})

    api.delete_unseen('demo')  # The DELETED change is never delivered.
    assert wait_until(lambda: operator.internal_state.replicas('demo') == 0)

    api.apply('next', 1)  # Watching works again after the new list.
    assert wait_until(lambda: status(api, 'next') == {'replicas': 1, 'ready': True})
