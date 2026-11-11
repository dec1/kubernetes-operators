# Minimal and library controllers

**The same control loop as [control_loop/](../control_loop/readme.md), at the two extremes of how much code is ours: everything by hand in one file, or almost everything from a library.** Both use the Widget replicas example, the terms spec, internal state and result, and the step numbers 1 to 4 of [Kubernetes Operators](../readme.md).

## Three levels, in reading order

**`reconcile()` and the Internal state are our code at every level. The levels differ in who supplies everything around them: the API client, the watch, the cache, the Queue and the Worker.** Each level removes what the one before it lacks. What each level is: [Project Overview](../readme.md#project-overview).

1. **Minimal**: [controller/python/controller.py](controller/python/controller.py). The whole mechanism reads from top to bottom: "LIST", "WATCH", `reconcile()`, status `PATCH`.
   *Lacks:* a Queue and a Worker. `reconcile()` runs on the thread that reads the watch and pauses there after each change, so while one Widget progresses, no other change is read. No tests.
2. **Structured**: [control_loop/](../control_loop/readme.md). Adds the Queue, the Worker and tests. The Watcher only updates the cache and adds names to the Queue; the Worker calls `reconcile()`. The pause between two passes for one Widget is a delayed Queue entry, so other Widgets are handled meanwhile.
   *Lacks:* what every operator needs and we would have to write again each time: retry delays that grow, several workers in parallel, leader election (several replicas of the operator, one active) and metrics.
3. **Library**: [controller/go/controller.go](controller/go/controller.go). _controller-runtime_ supplies the parts of level 2 and what level 2 lacks.
   *Lacks:* visibility. The mechanism is inside the library, which is why this level comes last: levels 1 and 2 show what the library does.

## Who supplies each part

| part | 1. minimal | 2. control_loop/, as operator | 3. library |
|---|---|---|---|
| API client | ours, a few lines | ours | the library |
| Notifier: "LIST", "WATCH", "LIST" again after 410 Gone ([explained](../api_server/readme.md#list-then-watch)) | ours, inline in `run()` | ours: the Watcher | the library, which calls this part the reflector |
| Store: the cache of Widget objects | ours: a dictionary. `reconcile()` reads it and calls the API directly | ours: Store (operator). `reconcile()` reaches it only through the two `WidgetStore` methods, which is what allows the web service variant | the library, which calls this part the informer cache |
| Queue and Worker | none | ours: one Worker | the library, which calls the Queue the work queue: one worker by default, several possible |
| `reconcile()` and Internal state | ours | ours | ours |
| next pass while the spec is not met | the status `PATCH` comes back from the watch as a MODIFIED change, which calls `reconcile()` again | the Worker adds the name to the Queue again after a delay | `Reconcile` returns `RequeueAfter`; changes that leave the spec unchanged, such as our own status `PATCH`, are filtered out |
| after an error | everything is listed again | only that name is retried, after a fixed delay | only that name is retried, after a growing delay |
| tests | none | each part, and the whole operator against a stand-in for the API server | none |

## Run and test them

Each level's own readme shows how: [minimal](controller/python/readme.md), [control_loop/ in Python](../control_loop/python/readme.md), [control_loop/ in Go](../control_loop/go/readme.md), [library](controller/go/readme.md).
