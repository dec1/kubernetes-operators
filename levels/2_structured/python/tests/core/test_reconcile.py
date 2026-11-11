"""Unit tests for reconcile(): the decision made in one pass.

Tested: for each situation in the readme's table (spec raised, lowered, met,
deleted; internal state changed elsewhere; action failed), what one call to
reconcile() changes in the internal state, which result it writes, and whether
it asks for another pass.

Not tested here: the Queue, the Worker's scheduling and retry (see
test_worker.py), threads, HTTP (see test_smoke.py).
These tests call reconcile() directly, with a Store and internal state set up
by each test, so nothing waits and nothing runs in the background.
"""

import pytest

from control_loop.core.internal_state import InternalState
from control_loop.core.reconcile import reconcile
from control_loop.web_service.store import Store


class FailingInternalState(InternalState):
    """Internal state whose actions fail, as a real system's sometimes do."""

    def add_replica(self, name: str) -> None:
        raise RuntimeError('cannot start replica')


@pytest.fixture
def store() -> Store:
    return Store(notify=lambda name: None)  # No Queue in these tests.


@pytest.fixture
def internal_state() -> InternalState:
    return InternalState()


def with_replicas(internal_state: InternalState, count: int) -> None:
    """Set up running replicas for 'demo' before the test's reconcile()."""
    for _ in range(count):
        internal_state.add_replica('demo')


def result(store: Store) -> object:
    """The result reconcile() last wrote for 'demo'."""
    widget = store.get('demo')
    assert widget is not None
    return widget['result']


def test_spec_raised_adds_one_replica_and_asks_for_another_pass(
    store: Store, internal_state: InternalState
) -> None:
    store.put_spec('demo', {'replicas': 3})
    assert reconcile('demo', store, internal_state) is True
    assert internal_state.replicas('demo') == 1
    assert result(store) == {'replicas': 1, 'ready': False}


def test_spec_lowered_removes_one_replica(store: Store, internal_state: InternalState) -> None:
    store.put_spec('demo', {'replicas': 1})
    with_replicas(internal_state, 3)
    assert reconcile('demo', store, internal_state) is True
    assert internal_state.replicas('demo') == 2


def test_spec_met_changes_nothing_and_reports_ready(
    store: Store, internal_state: InternalState
) -> None:
    store.put_spec('demo', {'replicas': 2})
    with_replicas(internal_state, 2)
    assert reconcile('demo', store, internal_state) is False
    assert internal_state.replicas('demo') == 2
    assert result(store) == {'replicas': 2, 'ready': True}


def test_repeated_passes_reach_the_spec_one_replica_at_a_time(
    store: Store, internal_state: InternalState
) -> None:
    store.put_spec('demo', {'replicas': 3})
    passes_needing_more = 0
    while reconcile('demo', store, internal_state):
        passes_needing_more += 1
    assert passes_needing_more == 2  # 0->1, 1->2; the pass reaching 3 returns False.
    assert result(store) == {'replicas': 3, 'ready': True}


def test_spec_deleted_removes_internal_state(store: Store, internal_state: InternalState) -> None:
    store.put_spec('demo', {'replicas': 2})
    with_replicas(internal_state, 1)
    store.delete('demo')
    assert reconcile('demo', store, internal_state) is False
    assert internal_state.replicas('demo') == 0


def test_internal_state_changed_elsewhere_is_corrected(
    store: Store, internal_state: InternalState
) -> None:
    # Drift: something other than reconcile() changed the internal state.
    store.put_spec('demo', {'replicas': 2})
    with_replicas(internal_state, 5)
    while reconcile('demo', store, internal_state):
        pass
    assert internal_state.replicas('demo') == 2


def test_failed_action_raises_and_writes_no_result(store: Store) -> None:
    store.put_spec('demo', {'replicas': 1})
    with pytest.raises(RuntimeError):
        reconcile('demo', store, FailingInternalState())
    assert result(store) is None
