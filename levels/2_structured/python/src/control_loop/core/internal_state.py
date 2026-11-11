"""Internal state: the running replicas of each Widget.

This stands in for the real thing a control loop manages, eg processes or
cloud machines. Here it is a counter per name in memory. Only reconcile()
changes it, and only one Worker runs, so it needs no lock.
"""


class InternalState:
    """Running replicas by name. A real system would start and stop
    processes here; this one only counts."""

    def __init__(self) -> None:
        self._replicas: dict[str, int] = {}  # name -> running replicas

    def replicas(self, name: str) -> int:
        """Running replicas for the name; 0 if it has none."""
        return self._replicas.get(name, 0)

    def add_replica(self, name: str) -> None:
        """Start one more replica (here: count up)."""
        self._replicas[name] = self.replicas(name) + 1

    def remove_replica(self, name: str) -> None:
        """Stop one replica (here: count down)."""
        self._replicas[name] = self.replicas(name) - 1

    def remove(self, name: str) -> None:
        """Remove everything for the name; no effect if it has nothing."""
        self._replicas.pop(name, None)
