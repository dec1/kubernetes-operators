"""HTTP API (web service): how a Client writes specs and reads results.

Kubernetes counterpart: the API server. It only reads and writes the Store;
it never calls reconcile() or changes the internal state.

    PUT    /widgets/{name}   body {"spec": {"replicas": 3}}  -> 200, the Widget
    GET    /widgets/{name}                                   -> 200, the Widget
    DELETE /widgets/{name}                                   -> 204

The returned Widget is {"name": ..., "spec": ..., "result": ...}; "result" is
null until reconcile() has written one.
"""

import http.server
import json
import re
from typing import Any

from control_loop.web_service.store import Store
from control_loop.core.widget import Spec

WIDGET_PATH = re.compile(r'^/widgets/([a-z0-9-]+)$')


def make_handler(store: Store) -> type[http.server.BaseHTTPRequestHandler]:
    """Return a request handler class that uses this Store.

    http.server creates a new handler object for every request and passes it
    no context of ours, so the class is defined here, where it can see store.
    """

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_PUT(self) -> None:
            name = self._name_or_404()
            if name is None:
                return
            spec = self._read_spec()
            if spec is None:
                self._send(400, {'error': 'body must be {"spec": {"replicas": <integer >= 0>}}'})
                return
            store.put_spec(name, spec)
            self._send_widget(name)

        def do_GET(self) -> None:
            name = self._name_or_404()
            if name is not None:
                self._send_widget(name)

        def do_DELETE(self) -> None:
            name = self._name_or_404()
            if name is None:
                return
            if store.delete(name):
                self._send(204, None)
            else:
                self._send(404, {'error': 'widget not found'})

        def _name_or_404(self) -> str | None:
            """Return the name from /widgets/{name}; otherwise send 404."""
            match = WIDGET_PATH.match(self.path)
            if match is None:
                self._send(404, {'error': 'use /widgets/{name}, name of a-z, 0-9 and -'})
                return None
            return match.group(1)

        def _read_spec(self) -> Spec | None:
            """Return the spec from the body, or None if invalid."""
            try:
                length = int(self.headers.get('Content-Length', 0))
                body = json.loads(self.rfile.read(length))
                replicas = body['spec']['replicas']
            except (ValueError, KeyError, TypeError):
                return None
            # bool is a subclass of int in Python; reject true/false.
            if type(replicas) is not int or replicas < 0:
                return None
            return {'replicas': replicas}

        def _send_widget(self, name: str) -> None:
            """Send the Widget as stored now, or 404 if it does not exist."""
            widget = store.get(name)
            if widget is None:
                self._send(404, {'error': 'widget not found'})
            else:
                self._send(200, {'name': name, **widget})

        def _send(self, status: int, body: dict[str, Any] | None) -> None:
            """Send a status code and, unless body is None, a JSON body."""
            self.send_response(status)
            if body is None:
                self.end_headers()
                return
            data = json.dumps(body).encode()
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, format: str, *args: Any) -> None:
            pass  # Keep the output to the control loop's own log lines.

    return Handler
