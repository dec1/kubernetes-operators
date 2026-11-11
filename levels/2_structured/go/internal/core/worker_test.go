// Unit tests for the Worker: how it schedules the next pass.
//
// Tested: after a failed pass the name is added again after retryDelay (not
// stepDelay), and the next pass continues; once the spec is met, the name is
// not added again.
//
// Not tested here: what Reconcile decides (see reconcile_test.go) and HTTP
// (see the webservice smoke test). The Worker runs in a goroutine, with a real
// Store and Queue and no HTTP API.
package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"controlloop/internal/core"
	"controlloop/internal/testutil"
	"controlloop/internal/webservice"
)

// failOnceState is an internal state whose first AddReplica fails; later ones
// succeed. It locks its counter, because the test reads it while the Worker
// changes it.
type failOnceState struct {
	core.InternalState
	mu       sync.Mutex
	failures int
}

func (s *failOnceState) AddReplica(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures == 0 {
		s.failures++
		return errors.New("cannot start replica")
	}
	return s.InternalState.AddReplica(name)
}

func (s *failOnceState) Failures() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failures
}

// startWorker runs the Worker in a goroutine with the given internal state and
// delays; it is stopped when the test ends.
func startWorker(t *testing.T, state core.InternalState, stepDelay, retryDelay time.Duration) (*core.Queue, *webservice.Store) {
	t.Helper()
	queue := core.NewQueue()
	store := webservice.NewStore(queue.Add)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		core.RunWorker(ctx, queue, store, state, stepDelay, retryDelay)
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return queue, store
}

func TestFailedPassIsRetriedAfterRetryDelay(t *testing.T) {
	// stepDelay 10 s, retryDelay 10 ms: finishing within WaitUntil's 5 s shows
	// the Worker waited retryDelay after the failure, not stepDelay.
	state := &failOnceState{InternalState: core.NewInternalState()}
	_, store := startWorker(t, state, 10*time.Second, 10*time.Millisecond)
	store.PutSpec("demo", core.Spec{Replicas: 1}) // One step, after the failure.
	if !testutil.WaitUntil(func() bool { return state.Replicas("demo") == 1 }) {
		t.Fatal("replicas never reached 1")
	}
	if got := state.Failures(); got != 1 {
		t.Errorf("failures = %d, want 1", got)
	}
}

func TestNameIsNotAddedAgainOnceTheSpecIsMet(t *testing.T) {
	// stepDelay 10 ms: a wrong "check again" would arrive well within the
	// 200 ms that Take waits below.
	state := &failOnceState{InternalState: core.NewInternalState(), failures: 1} // No failure.
	if err := state.AddReplica("demo"); err != nil {                             // Already meets the spec below.
		t.Fatal(err)
	}
	queue, store := startWorker(t, state, 10*time.Millisecond, 10*time.Millisecond)
	store.PutSpec("demo", core.Spec{Replicas: 1})
	ready := func() bool {
		widget, _ := store.Get("demo")
		return widget.Result != nil && *widget.Result == core.Result{Replicas: 1, Ready: true}
	}
	if !testutil.WaitUntil(ready) {
		t.Fatal("result never became ready")
	}
	if name, ok := queue.Take(200 * time.Millisecond); ok {
		t.Errorf("%q was scheduled again", name)
	}
}
