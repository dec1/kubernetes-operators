package core

// Reconcile is one pass of the control loop for one Widget.
//
// It compares the spec with the internal state, changes the internal state by
// at most one replica, and writes the result. The same comparison handles every
// situation (created, raised, lowered, deleted, already met), so there are no
// separate code paths per event.
//
// It works with either variant's Store: it uses only the two methods of
// WidgetStore. It does not touch the Queue: it returns whether another pass is
// needed, and the Worker adds the name again. If an action fails, the error
// reaches the Worker, which also adds the name again.
//
// Comments 1 to 4 mark the control loop's four steps, numbered as in the readme.
func Reconcile(name string, store WidgetStore, state InternalState) (needsAnotherPass bool, err error) {
	widget, exists := store.Get(name)

	// SPEC DELETED: remove the internal state. There is no result to write,
	// because the Widget no longer exists in the Store.
	if !exists {
		state.Remove(name)
		return false, nil
	}

	// 1. SPEC: read what the Client asked for.
	specifiedReplicas := widget.Spec.Replicas

	// 2. INTERNAL STATE: measure it.
	runningReplicas := state.Replicas(name)

	// 3. Change the internal state by at most one replica per pass.
	switch {
	case runningReplicas < specifiedReplicas:
		err = state.AddReplica(name)
	case runningReplicas > specifiedReplicas:
		err = state.RemoveReplica(name)
	}
	if err != nil {
		return false, err
	}

	// 4. RESULT: report what is running now, read again after the action.
	runningReplicas = state.Replicas(name)
	ready := runningReplicas == specifiedReplicas
	if err := store.SetResult(name, Result{Replicas: runningReplicas, Ready: ready}); err != nil {
		return false, err
	}
	return !ready, nil
}
