"""Queue: names waiting for a check, first in, first out.

A name only means "check this Widget"; the Worker reads the current spec when
it takes the name. A name already waiting is not added again, so several spec
writes before a check lead to one check.
"""

import collections
import threading


class Queue:
    """Thread-safe FIFO of names; a name is waiting at most once."""

    def __init__(self) -> None:
        self._names: collections.deque[str] = collections.deque()  # Waiting names, in order.
        self._waiting: set[str] = set()  # Same names, for a fast duplicate check.
        self._changed = threading.Condition()

    def add(self, name: str) -> None:
        """Add the name unless it is already waiting."""
        with self._changed:
            if name in self._waiting:
                return
            self._waiting.add(name)
            self._names.append(name)
            self._changed.notify()  # Wake a Worker waiting in take().

    def add_after(self, name: str, delay_seconds: float) -> None:
        """Add the name after a delay, without blocking the caller.
        Used by the Worker for "check again shortly"."""
        timer = threading.Timer(delay_seconds, self.add, args=[name])
        timer.daemon = True  # Do not keep the process alive at exit.
        timer.start()

    def take(self, timeout_seconds: float) -> str | None:
        """Remove and return the oldest waiting name.
        Waits up to timeout_seconds; returns None if none arrives."""
        with self._changed:
            if not self._changed.wait_for(lambda: self._names, timeout_seconds):
                return None
            name = self._names.popleft()
            self._waiting.discard(name)  # It may now be added again.
            return name
