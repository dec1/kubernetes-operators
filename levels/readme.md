# Minimal and Library controllers

**This directory holds the three levels; this page compares them part by part.** What each level is, and its strengths and limits, is in [Project Overview](../readme.md#project-overview).

- [**Minimal**](1_minimal/readme.md): the whole loop in one Python file.
- [**Library**](3_library/readme.md): the same loop on _controller-runtime_.

The level between them is [**Structured**](2_structured/readme.md). All three use the Widget replicas example, the terms spec, internal state and result, and the step numbers 1 to 4 of [Kubernetes Operators](../readme.md).

## Who supplies each part

**`reconcile()` and the Internal state are our code at every level. The levels differ in who supplies everything around them: the API client, the watch, the cache, the Queue and the Worker.**

| part | Minimal | Structured, as operator | Library |
|---|---|---|---|
| API client | ours, a few lines | ours | _controller-runtime_ |
| Notifier: "LIST", "WATCH", "LIST" again after 410 Gone ([explained](../api_server/readme.md#list-then-watch)) | ours, inline in `run()` | ours: the Watcher | _controller-runtime_, which calls this part the reflector |
| Store: the cache of Widget objects | ours: a dictionary. `reconcile()` reads it and calls the API directly | ours: Store (operator). `reconcile()` reaches it only through the two `WidgetStore` methods, which is what allows the web service variant | _controller-runtime_, which calls this part the informer cache |
| Queue and Worker | none. `reconcile()` runs on the thread that reads the watch and pauses there after each change, so no other change is read meanwhile | ours: one Worker. The pause between two passes for one Widget is a delayed Queue entry, so other Widgets are handled meanwhile | _controller-runtime_, which calls the Queue the work queue: one worker by default, several possible |
| `reconcile()` and Internal state | ours | ours | ours; the function is named `Reconcile` |
| next pass while the spec is not met | the status `PATCH` comes back from the watch as a MODIFIED change, which calls `reconcile()` again | the Worker adds the name to the Queue again after a delay | `Reconcile` returns `RequeueAfter`; changes that leave the spec unchanged, such as our own status `PATCH`, are filtered out |
| after an error | everything is listed again | only that name is retried, after a fixed delay | only that name is retried, after a growing delay |
| tests | none | each part, and the whole operator against a stand-in for the API server | none |

**Why Library comes last in the reading order.** Its mechanism is inside _controller-runtime_, not visible in our code. Minimal and Structured show what _controller-runtime_ does.

## Run and test them

Each level's own readme shows how: [Minimal](1_minimal/readme.md), [Structured in Python](2_structured/python/readme.md), [Structured in Go](2_structured/go/readme.md), [Library](3_library/readme.md).
