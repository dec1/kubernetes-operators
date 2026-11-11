"""API client: the three calls the operator makes to the Kubernetes API server.

    list     GET   .../widgets                         all Widget objects + position
    watch    GET   .../widgets?watch=true&...          changes after a position
    status   PATCH .../widgets/{name}/status           write the result

KubernetesApi states these calls; HttpKubernetesApi makes them over HTTP, and
the tests use an in-memory fake with the same methods (fake_api.py).

A position is the resourceVersion: an opaque string marking a point in the API
server's change history. The server keeps that history only briefly; a watch
from a position it no longer has fails with 410 Gone (ResourceExpired), and
only a fresh list recovers.
"""

import json
import os
import ssl
from collections.abc import Iterator
from pathlib import Path
from typing import Any, Protocol
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

from control_loop.core.widget import Result

KubernetesObject = dict[str, Any]  # A Widget object as JSON: metadata, spec, status.
WatchEvent = dict[str, Any]  # {"type": "ADDED" | "MODIFIED" | "DELETED" | "BOOKMARK", "object": ...}


class ResourceExpired(Exception):
    """410 Gone: the changes after this position are no longer available."""


class NotFound(Exception):
    """404: the object does not exist (any more)."""


class KubernetesApi(Protocol):
    """What the operator needs from the API server, for one namespace."""

    def list_widgets(self) -> tuple[list[KubernetesObject], str]:
        """All Widget objects, and the position (resourceVersion) of this list."""
        ...

    def watch_widgets(self, resource_version: str, timeout_seconds: float) -> Iterator[WatchEvent]:
        """Yield each change after resource_version as it happens, for up to
        timeout_seconds. Raises ResourceExpired if that position is too old."""
        ...

    def patch_status(self, name: str, status: Result) -> KubernetesObject:
        """Write the object's status; return the updated object.
        Raises NotFound if the object does not exist."""
        ...


class HttpKubernetesApi:
    """KubernetesApi over HTTP, standard library only.

    Inside a Pod: uses the Pod's ServiceAccount token and CA bundle.
    Elsewhere: talks plain HTTP to `kubectl proxy` (default 127.0.0.1:8001),
    which adds your kubeconfig credentials. No kubeconfig parsing needed here.

    Not covered by the tests: they use fake_api.py instead.
    """

    SERVICE_ACCOUNT = Path('/var/run/secrets/kubernetes.io/serviceaccount')

    def __init__(self, base_url: str, namespace: str, in_cluster: bool) -> None:
        self._widgets_url = f'{base_url}/apis/example.com/v1/namespaces/{namespace}/widgets'
        self._in_cluster = in_cluster
        self._tls = (
            ssl.create_default_context(cafile=str(self.SERVICE_ACCOUNT / 'ca.crt'))
            if in_cluster
            else None
        )

    @classmethod
    def from_environment(cls, namespace: str) -> 'HttpKubernetesApi':
        """In a Pod, Kubernetes sets KUBERNETES_SERVICE_HOST; otherwise use kubectl proxy."""
        host = os.environ.get('KUBERNETES_SERVICE_HOST')
        if host:
            port = os.environ.get('KUBERNETES_SERVICE_PORT', '443')
            return cls(f'https://{host}:{port}', namespace, in_cluster=True)
        return cls(os.environ.get('KUBERNETES_API_URL', 'http://127.0.0.1:8001'), namespace, False)

    def list_widgets(self) -> tuple[list[KubernetesObject], str]:
        with self._open('GET', self._widgets_url) as response:
            body = json.load(response)
        return body['items'], body['metadata']['resourceVersion']

    def watch_widgets(self, resource_version: str, timeout_seconds: float) -> Iterator[WatchEvent]:
        query = urlencode(
            {
                'watch': 'true',
                'resourceVersion': resource_version,  # Send changes after this position.
                'timeoutSeconds': int(timeout_seconds),  # The server ends the response then.
                'allowWatchBookmarks': 'true',  # Fresh positions even when nothing changes.
            }
        )
        try:
            with self._open('GET', f'{self._widgets_url}?{query}') as response:
                for line in response:  # One JSON event per line; waits for the next.
                    if not line.strip():
                        continue
                    event: WatchEvent = json.loads(line)
                    if event['type'] == 'ERROR':
                        if event['object'].get('code') == 410:
                            raise ResourceExpired(event['object'].get('message', ''))
                        raise RuntimeError(f'watch error: {event["object"]}')
                    yield event
        except HTTPError as error:
            if error.code == 410:
                raise ResourceExpired(str(error)) from error
            raise

    def patch_status(self, name: str, status: Result) -> KubernetesObject:
        try:
            with self._open('PATCH', f'{self._widgets_url}/{name}/status', {'status': status}) as response:
                object_: KubernetesObject = json.load(response)
                return object_
        except HTTPError as error:
            if error.code == 404:
                raise NotFound(name) from error
            raise

    def _open(self, method: str, url: str, body: dict[str, Any] | None = None) -> Any:
        headers = {'Accept': 'application/json'}
        if self._in_cluster:
            # Read on every call: Kubernetes rotates the token file.
            headers['Authorization'] = 'Bearer ' + (self.SERVICE_ACCOUNT / 'token').read_text().strip()
        data = None
        if body is not None:
            headers['Content-Type'] = 'application/merge-patch+json'
            data = json.dumps(body).encode()
        request = Request(url, data=data, headers=headers, method=method)
        return urlopen(request, context=self._tls, timeout=60)
