"""reconcile(name): one pass of the control loop for one Widget.

Compare the spec with the internal state, change the internal state by at most
one replica, and write the result. The same comparison handles every situation
(created, raised, lowered, deleted, already met), so there are no separate code
paths per event.

reconcile() works with either variant's Store: it uses only the two methods
of WidgetStore (see widget.py). It does not touch the Queue: it returns whether
another pass is needed, and the Worker adds the name again. If an action fails,
the exception reaches the Worker, which also adds the name again.

Comments 1 to 4 mark the control loop's four steps, numbered as in the readme.
"""

from control_loop.core.internal_state import InternalState
from control_loop.core.widget import WidgetStore


def reconcile(name: str, store: WidgetStore, internal_state: InternalState) -> bool:
    """Return True if the internal state does not yet meet the spec."""
    widget = store.get(name)

    # SPEC DELETED: remove the internal state. There is no result to write,
    # because the Widget no longer exists in the Store.
    if widget is None:
        internal_state.remove(name)
        return False

    # 1. SPEC: read what the Client asked for.
    specified_replicas = widget['spec']['replicas']

    # 2. INTERNAL STATE: measure it.
    running_replicas = internal_state.replicas(name)

    # 3. Change the internal state by at most one replica per pass.
    if running_replicas < specified_replicas:
        internal_state.add_replica(name)
    elif running_replicas > specified_replicas:
        internal_state.remove_replica(name)

    # 4. RESULT: report what is running now, read again after the action.
    running_replicas = internal_state.replicas(name)
    ready = running_replicas == specified_replicas
    store.set_result(name, {'replicas': running_replicas, 'ready': ready})
    return not ready
