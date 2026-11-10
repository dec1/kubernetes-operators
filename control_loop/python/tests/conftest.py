"""Shared test helpers; pytest makes the fixtures here available to every test file."""

import time
from collections.abc import Callable

import pytest

WaitUntil = Callable[[Callable[[], bool]], bool]  # Type of the fixture below.


@pytest.fixture
def wait_until() -> WaitUntil:
    """Return a function that polls condition() until it is true, for up to 5 s.

    Used where a background thread does the work, instead of fixed sleeps:
    quick when the machine is fast, still passing when it is slow.
    """

    def wait(condition: Callable[[], bool]) -> bool:
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if condition():
                return True
            time.sleep(0.01)
        return False

    return wait
