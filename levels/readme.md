# Levels

As [introduced](../readme.md#project-overview), the example code implements the same control loop at three _levels_:

- [**_Minimal_**](1_minimal/readme.md): the whole loop in one Python file.
- [**_Structured_**](2_structured/readme.md): the same loop as separate named parts with tests, in Python and Go. Each can also run as a web service without Kubernetes, for comparison.
- [**_Library_**](3_library/readme.md): the same loop on _controller-runtime_.

All three use the _Widget_ replicas example, the terms spec, internal state and result, and the step numbers 1 to 4 of [Kubernetes Operators](../readme.md).

## Which parts are our code

An operator is made of the parts in the first column. `reconcile()` and the Internal state are our code at every level. For the other parts, the levels differ: they are our code, or they come from _controller-runtime_. The Structured column describes its Kubernetes operator variant.

| part of an operator | Minimal | Structured | Library |
|---|---|---|---|
| API client: makes the calls to the API server | ours, a few lines | ours | _controller-runtime_ |
| Notifier: finds out which Widgets changed, by "LIST" and "WATCH" ([explained](../api_server/readme.md#list-then-watch)) | ours, inline in `run()` | ours: the Watcher | _controller-runtime_, which calls this part the reflector |
| Store: the cache of Widget objects | ours: a dictionary. `reconcile()` reads it and calls the API directly | ours: Store (operator). `reconcile()` reaches it only through the two `WidgetStore` methods, which is what allows the web service variant | _controller-runtime_, which calls this part the informer cache |
| Queue and Worker: the Queue holds the names of the Widgets to check; the Worker calls `reconcile()` for one name at a time | none. `reconcile()` runs on the thread that reads the watch and pauses there after each change, so no other change is read meanwhile | ours: one Worker. The pause between two passes for one Widget is a delayed Queue entry, so other Widgets are handled meanwhile | _controller-runtime_, which calls the Queue the work queue: one worker by default, several possible |
| `reconcile()` and Internal state: one pass of the loop for one Widget, and what that pass changes | ours | ours | ours; the function is named `Reconcile` |

> _Why Library comes last in the reading order:_
> Its mechanism is inside _controller-runtime_, not visible in our code. Minimal and Structured show what _controller-runtime_ does.

## Other differences

| | Minimal | Structured | Library |
|---|---|---|---|
| next pass while the spec is not met | the status `PATCH` comes back from the watch as a MODIFIED change, which calls `reconcile()` again | the Worker adds the name to the Queue again after a delay | `Reconcile` returns `RequeueAfter`; changes that leave the spec unchanged, such as our own status `PATCH`, are filtered out |
| after an error | everything is listed again | only that name is retried, after a fixed delay | only that name is retried, after a growing delay |
| tests | none | each part, and the whole operator against a stand-in for the API server | none |

## Run and test them

Each level's own readme shows how: [Minimal](1_minimal/readme.md), [Structured in Python](2_structured/python/readme.md), [Structured in Go](2_structured/go/readme.md), [Library](3_library/readme.md).
