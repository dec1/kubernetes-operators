"""Minimal controller: the control loop in one file, Python standard library only.

The same Widget replicas example as Structured, with the same terms:
- spec: the client's request in a Widget object's spec, eg replicas: 3;
- internal state: the running replicas per name, a dictionary in memory;
- result: what the controller reports, written to the object's status.

What it leaves out compared with Structured: no Queue and no Worker.
reconcile() runs inline, on the same thread that reads the watch, so a slow
pass delays every other Widget. No WidgetStore boundary: reconcile() reads the
cache and calls the API directly, so there is no web service variant. No tests.

Comments use "LIST" and "WATCH" as labels for the two kinds of read; both are
HTTP GET requests.

Run it outside the cluster (see manifests/readme.md for the CRD and a sample):
    kubectl proxy &
    python3 controller.py
"""

import json
import os
import ssl
import time
from pathlib import Path
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

STEP_DELAY_SECONDS = 1.0  # Pause after each change, so the progress is visible.
RETRY_DELAY_SECONDS = 2.0  # Pause before listing again after an error.


class API:
    """Calls to the API server: in a Pod with its ServiceAccount, elsewhere
    through `kubectl proxy`, which adds your kubeconfig credentials."""

    SERVICE_ACCOUNT = Path('/var/run/secrets/kubernetes.io/serviceaccount')

    def __init__(self):
        host = os.environ.get('KUBERNETES_SERVICE_HOST')  # Set by Kubernetes in a Pod.
        if host:
            if ':' in host:
                host = f'[{host}]'  # IPv6 address.
            port = os.environ.get('KUBERNETES_SERVICE_PORT', '443')
            self.base = f'https://{host}:{port}'
            self.in_cluster = True
            self.tls = ssl.create_default_context(cafile=str(self.SERVICE_ACCOUNT / 'ca.crt'))
        else:
            self.base = 'http://127.0.0.1:8001'
            self.in_cluster = False
            self.tls = None

    def open(self, method, path, body=None):
        headers = {'Accept': 'application/json'}
        if self.in_cluster:
            # Read on every call: Kubernetes rotates the token file.
            token = (self.SERVICE_ACCOUNT / 'token').read_text().strip()
            headers['Authorization'] = 'Bearer ' + token
        payload = None
        if body is not None:
            headers['Content-Type'] = 'application/merge-patch+json'
            payload = json.dumps(body).encode()
        request = Request(self.base + path, data=payload, headers=headers, method=method)
        return urlopen(request, context=self.tls, timeout=60)

    def call(self, method, path, body=None):
        with self.open(method, path, body) as response:
            return json.load(response)


def widgets_url(namespace):
    return f'/apis/example.com/v1/namespaces/{quote(namespace, safe="")}/widgets'


def reconcile(api, namespace, name, cache, internal_state):
    """One pass of the control loop for one Widget: compare its cached spec
    with the internal state, change the internal state by at most one
    replica, and write the result to status if it changed.

    Receives a name, not an event, so creation, change and deletion all take
    this one path.
    """
    widget_object = cache.get(name)

    # SPEC DELETED: object gone or marked for deletion. Remove the internal
    # state (a dictionary entry); no Kubernetes object is deleted here.
    if widget_object is None or widget_object['metadata'].get('deletionTimestamp'):
        if internal_state.pop(name, None) is not None:
            print(f'{name}: spec deleted, internal state removed', flush=True)
        return

    # 1. SPEC: read what the client asked for.
    specified_replicas = widget_object['spec']['replicas']

    # 2. INTERNAL STATE: measure it.
    running_replicas = internal_state.get(name, 0)

    # 3. Change the internal state by at most one replica per pass.
    if running_replicas != specified_replicas:
        running_replicas += 1 if running_replicas < specified_replicas else -1
        internal_state[name] = running_replicas
        time.sleep(STEP_DELAY_SECONDS)

    # 4. RESULT: write it to status, only if it changed.
    # Repetition comes from Kubernetes itself: this PATCH changes the object,
    # so the watch delivers it back as MODIFIED, which calls reconcile() again.
    # Once the result no longer changes, there is no PATCH and the passes stop.
    result = {'replicas': running_replicas, 'ready': running_replicas == specified_replicas}
    if widget_object.get('status') != result:
        api.call('PATCH', f'{widgets_url(namespace)}/{quote(name, safe="")}/status', {'status': result})
        print(f'{name}: result {result}', flush=True)


def watch(api, namespace, resource_version):
    """Yield each change after resource_version, as it happens, for up to 30 s.

    Each event has a type (ADDED, MODIFIED, DELETED) and the object as it is
    now. MODIFIED can be a spec, status or metadata change. BOOKMARK carries
    only a newer resourceVersion, so a quiet watch can still resume.
    """
    query = urlencode({
        'watch': 'true',
        'resourceVersion': resource_version,  # Send changes after this position.
        'timeoutSeconds': 30,  # The server ends the response then.
        'allowWatchBookmarks': 'true',
    })
    with api.open('GET', f'{widgets_url(namespace)}?{query}') as response:
        for line in response:  # One JSON event per line; waits for the next.
            if not line.strip():
                continue
            event = json.loads(line)
            if event['type'] == 'ERROR':  # eg 410 Gone: that position is too old.
                raise RuntimeError(f'watch error: {event["object"]}')
            yield event


def run(api, namespace):
    internal_state = {}  # INTERNAL STATE: running replicas per name.

    while True:
        try:
            # "LIST": GET all Widget objects into a fresh cache (name -> object).
            listed = api.call('GET', widgets_url(namespace))
            cache = {item['metadata']['name']: item for item in listed['items']}

            # Reconcile every listed name, and every name in the internal state
            # that is no longer listed: a deletion missed while not watching.
            for name in cache.keys() | internal_state.keys():
                reconcile(api, namespace, name, cache, internal_state)

            # "WATCH": GET with watch=true, starting at the list's position.
            # Each change updates the cache and reconciles that name. When the
            # response ends at its timeout, watch again from the last position.
            resource_version = listed['metadata']['resourceVersion']
            while True:
                for event in watch(api, namespace, resource_version):
                    widget_object = event['object']
                    resource_version = widget_object['metadata']['resourceVersion']
                    if event['type'] == 'BOOKMARK':
                        continue
                    name = widget_object['metadata']['name']
                    if event['type'] == 'DELETED':
                        cache.pop(name, None)
                    else:
                        cache[name] = widget_object
                    reconcile(api, namespace, name, cache, internal_state)
        except Exception as error:
            # Every error, a failed PATCH or 410 Gone alike, restarts from
            # "LIST". The internal state stays in memory and is compared again.
            print(f'retrying after error: {error}', flush=True)
            time.sleep(RETRY_DELAY_SECONDS)


if __name__ == '__main__':
    run(API(), os.environ.get('WATCH_NAMESPACE', 'default'))
