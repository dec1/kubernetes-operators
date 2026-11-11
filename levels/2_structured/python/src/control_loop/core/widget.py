"""The Widget data, and the one boundary between the shared core and a variant.

The shared core (reconcile(), Worker, Queue, Internal state) only needs to
read a Widget's spec and write its result. WidgetStore states exactly that.
Each variant supplies its own WidgetStore:

    web service           web_service/store.py: specs and results in memory
    Kubernetes operator   kubernetes_operator/store.py: Widget objects in Kubernetes

So the core never depends on a variant; a variant plugs into the core
(inversion of control).
"""

from typing import Protocol, TypedDict


class Spec(TypedDict):
    """What the Client asks for."""

    replicas: int


class Result(TypedDict):
    """What reconcile() last observed."""

    replicas: int
    ready: bool


class Widget(TypedDict):
    """A Widget as the core sees it: spec and latest result."""

    spec: Spec
    result: Result | None  # None until reconcile() has written one.


class WidgetStore(Protocol):
    """What the core needs from a Store. Any class with these two methods fits;
    it does not have to inherit from WidgetStore."""

    def get(self, name: str) -> Widget | None:
        """The Widget's current spec and result, or None if it does not exist."""
        ...

    def set_result(self, name: str, result: Result) -> None:
        """Record the result; ignored if the Widget no longer exists."""
        ...
