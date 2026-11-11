# Detail: the three requests as HTTP

**All three calls of [Operator and API server](readme.md) are ordinary HTTP requests to one base URL.** For Widgets in the namespace `default`, let `B = /apis/example.com/v1/namespaces/default`:

| call | request |
|---|---|
| "LIST" | `GET B/widgets` |
| "WATCH" | `GET B/widgets?watch=true&resourceVersion=R&timeoutSeconds=30&allowWatchBookmarks=true` |
| status `PATCH` | `PATCH B/widgets/demo/status`, with `Content-Type: application/merge-patch+json` and a body holding only `status` |

In `B`, `example.com` is the API group, `v1` the version, and `widgets` the resource type's plural name. `Widget` is its `kind` in YAML.

## The open watch response

The request and the start of its response in HTTP/1.1, with headers shortened:

```http
GET /apis/example.com/v1/namespaces/default/widgets?watch=true&resourceVersion=742 HTTP/1.1
Host: <api-server>
Authorization: Bearer <token>
```

```http
HTTP/1.1 200 OK
Content-Type: application/json
Transfer-Encoding: chunked
```

The body then grows by one JSON line whenever an object changes; minutes can pass between two lines. Objects shortened:

```text
{"type":"ADDED","object":{"metadata":{"name":"demo","resourceVersion":"743"},"spec":{"replicas":3}}}
{"type":"MODIFIED","object":{"metadata":{"name":"demo","resourceVersion":"751"},"spec":{"replicas":1}}}
```

- **One response, many notifications.** The response itself stays unfinished, not merely the connection under it. Each line is more data in that response, not a new request to the operator.
- **Chunk framing is the HTTP library's job.** `Transfer-Encoding: chunked` is how HTTP/1.1 sends a body of unknown length; HTTP/2 frames it differently. The code only reads lines.

## 410 Gone in two forms

A position that has expired is reported either as the HTTP status `410` of the watch request, or as a line of type `ERROR` with `"code": 410` inside a response that was already open. [api_client.py](../levels/2_structured/python/src/control_loop/kubernetes_operator/api_client.py) handles both the same way: list again.
