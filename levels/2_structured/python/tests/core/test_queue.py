"""Unit tests for the Queue: order, duplicates and delayed adds.

Tested: names come out in the order added; a name already waiting is not added
twice; a taken name can be added again; take() gives up after its timeout;
add_after() adds the name later, not at once.

Not tested here: several threads adding and taking at the same time, and the
exact length of add_after()'s delay (only "not at once, but within 2 s").
"""

import pytest

from control_loop.core.queue import Queue


@pytest.fixture
def queue() -> Queue:
    return Queue()


def test_names_are_taken_in_order(queue: Queue) -> None:
    queue.add('a')
    queue.add('b')
    assert queue.take(timeout_seconds=0) == 'a'
    assert queue.take(timeout_seconds=0) == 'b'


def test_a_waiting_name_is_not_added_twice(queue: Queue) -> None:
    queue.add('demo')
    queue.add('demo')
    assert queue.take(timeout_seconds=0) == 'demo'
    assert queue.take(timeout_seconds=0) is None


def test_a_taken_name_can_be_added_again(queue: Queue) -> None:
    queue.add('demo')
    queue.take(timeout_seconds=0)
    queue.add('demo')
    assert queue.take(timeout_seconds=0) == 'demo'


def test_take_returns_none_when_nothing_arrives(queue: Queue) -> None:
    assert queue.take(timeout_seconds=0.01) is None


def test_add_after_adds_the_name_later(queue: Queue) -> None:
    queue.add_after('demo', 0.01)
    assert queue.take(timeout_seconds=0) is None  # Not yet.
    assert queue.take(timeout_seconds=2) == 'demo'
