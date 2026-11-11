// Unit tests for Reconcile: the decision made in one pass.
//
// Tested: for each situation in the readme's table (spec raised, lowered, met,
// deleted; internal state changed elsewhere; action failed), what one call to
// Reconcile changes in the internal state, which result it writes, and whether
// it asks for another pass.
//
// Not tested here: the Queue, the Worker's scheduling and retry (see
// worker_test.go), goroutines, HTTP (see the webservice smoke test). These
// tests call Reconcile directly, with a Store and internal state set up by each
// test, so nothing waits and nothing runs in the background.
package core_test

import (
	"errors"
	"testing"

	"controlloop/internal/core"
	"controlloop/internal/webservice"
)

// failingState is an internal state whose actions fail, as a real system's
// sometimes do. Embedding gives it every other method unchanged.
type failingState struct{ core.InternalState }

func (failingState) AddReplica(string) error { return errors.New("cannot start replica") }

// setUp returns a Store holding 'demo' with the given spec (no Queue in these
// tests) and an internal state with the given running replicas for 'demo'.
func setUp(t *testing.T, specified, running int) (*webservice.Store, core.InternalState) {
	t.Helper()
	store := webservice.NewStore(func(string) {})
	store.PutSpec("demo", core.Spec{Replicas: specified})
	state := core.NewInternalState()
	for range running {
		if err := state.AddReplica("demo"); err != nil {
			t.Fatal(err)
		}
	}
	return store, state
}

// result returns the result Reconcile last wrote for 'demo'.
func result(t *testing.T, store *webservice.Store) *core.Result {
	t.Helper()
	widget, exists := store.Get("demo")
	if !exists {
		t.Fatal("demo does not exist")
	}
	return widget.Result
}

func mustReconcile(t *testing.T, store core.WidgetStore, state core.InternalState) bool {
	t.Helper()
	needsAnotherPass, err := core.Reconcile("demo", store, state)
	if err != nil {
		t.Fatal(err)
	}
	return needsAnotherPass
}

func TestSpecRaisedAddsOneReplicaAndAsksForAnotherPass(t *testing.T) {
	store, state := setUp(t, 3, 0)
	if !mustReconcile(t, store, state) {
		t.Error("want another pass")
	}
	if got := state.Replicas("demo"); got != 1 {
		t.Errorf("replicas = %d, want 1", got)
	}
	if got := result(t, store); *got != (core.Result{Replicas: 1, Ready: false}) {
		t.Errorf("result = %+v", *got)
	}
}

func TestSpecLoweredRemovesOneReplica(t *testing.T) {
	store, state := setUp(t, 1, 3)
	if !mustReconcile(t, store, state) {
		t.Error("want another pass")
	}
	if got := state.Replicas("demo"); got != 2 {
		t.Errorf("replicas = %d, want 2", got)
	}
}

func TestSpecMetChangesNothingAndReportsReady(t *testing.T) {
	store, state := setUp(t, 2, 2)
	if mustReconcile(t, store, state) {
		t.Error("want no other pass")
	}
	if got := state.Replicas("demo"); got != 2 {
		t.Errorf("replicas = %d, want 2", got)
	}
	if got := result(t, store); *got != (core.Result{Replicas: 2, Ready: true}) {
		t.Errorf("result = %+v", *got)
	}
}

func TestRepeatedPassesReachTheSpecOneReplicaAtATime(t *testing.T) {
	store, state := setUp(t, 3, 0)
	passesNeedingMore := 0
	for mustReconcile(t, store, state) {
		passesNeedingMore++
	}
	if passesNeedingMore != 2 { // 0->1, 1->2; the pass reaching 3 returns false.
		t.Errorf("passes needing more = %d, want 2", passesNeedingMore)
	}
	if got := result(t, store); *got != (core.Result{Replicas: 3, Ready: true}) {
		t.Errorf("result = %+v", *got)
	}
}

func TestSpecDeletedRemovesInternalState(t *testing.T) {
	store, state := setUp(t, 2, 1)
	store.Delete("demo")
	if mustReconcile(t, store, state) {
		t.Error("want no other pass")
	}
	if got := state.Replicas("demo"); got != 0 {
		t.Errorf("replicas = %d, want 0", got)
	}
}

func TestInternalStateChangedElsewhereIsCorrected(t *testing.T) {
	// Drift: something other than Reconcile changed the internal state.
	store, state := setUp(t, 2, 5)
	for mustReconcile(t, store, state) {
	}
	if got := state.Replicas("demo"); got != 2 {
		t.Errorf("replicas = %d, want 2", got)
	}
}

func TestFailedActionReturnsAnErrorAndWritesNoResult(t *testing.T) {
	store, state := setUp(t, 1, 0)
	if _, err := core.Reconcile("demo", store, failingState{state}); err == nil {
		t.Error("want an error")
	}
	if got := result(t, store); got != nil {
		t.Errorf("result = %+v, want none", *got)
	}
}
