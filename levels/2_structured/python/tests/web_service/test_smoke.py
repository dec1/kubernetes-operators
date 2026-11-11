"""Smoke test: run the whole process and use it over HTTP, as a Client would.

Tested: the parts are wired together correctly. A PUT is answered at once,
the result then reaches the spec one replica at a time, a lowered spec is
reached too, DELETE removes the Widget and its internal state, and an invalid
spec is rejected.

Not tested here: each decision in detail (see test_reconcile.py), the Worker's
retry after a failed pass (see test_worker.py), and behaviour under load.

Uses a free port (0) and a short step delay, and waits with the wait_until
fixture (conftest.py) instead of fixed sleeps.
"""

import json
import urllib.error
import urllib.request
from collections.abc import Callable, Iterator
from typing import Any

import pytest

from control_loop.web_service.main import WebService

WaitUntil = Callable[[Callable[[], bool]], bool]  # The conftest.py fixture.


@pytest.fixture
def loop() -> Iterator[WebService]:
    """A running control loop for one test; stopped afterwards."""
    loop = WebService(port=0, step_delay=0.01, retry_delay=0.01)
    loop.start()
    yield loop
    loop.stop()


def request(
    loop: WebService, method: str, name: str, body: dict[str, Any] | None = None
) -> tuple[int, Any]:
    """Send one HTTP request; return (status, parsed JSON body or None)."""
    url = f'http://127.0.0.1:{loop.port}/widgets/{name}'
    data = json.dumps(body).encode() if body is not None else None
    try:
        with urllib.request.urlopen(
            urllib.request.Request(url, data=data, method=method)
        ) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read() or b'null')


def result(loop: WebService, name: str) -> Any:
    """The result currently returned by GET /widgets/{name}."""
    return request(loop, 'GET', name)[1]['result']


def test_spec_changes_are_reached_and_deletion_cleans_up(
    loop: WebService, wait_until: WaitUntil
) -> None:
    status, widget = request(loop, 'PUT', 'demo', {'spec': {'replicas': 3}})
    assert status == 200
    assert widget == {'name': 'demo', 'spec': {'replicas': 3}, 'result': None}

    assert wait_until(lambda: result(loop, 'demo') == {'replicas': 3, 'ready': True})

    request(loop, 'PUT', 'demo', {'spec': {'replicas': 1}})
    assert wait_until(lambda: result(loop, 'demo') == {'replicas': 1, 'ready': True})

    assert request(loop, 'DELETE', 'demo')[0] == 204
    assert request(loop, 'GET', 'demo')[0] == 404
    assert wait_until(lambda: loop.internal_state.replicas('demo') == 0)


def test_invalid_spec_is_rejected(loop: WebService) -> None:
    status, body = request(loop, 'PUT', 'demo', {'spec': {'replicas': -1}})
    assert status == 400
    assert 'error' in body
