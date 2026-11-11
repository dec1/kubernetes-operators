package core

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// RunWorker takes names from the Queue and calls Reconcile for each, until ctx
// is cancelled; meant for its own goroutine.
//
// Shared by both variants. Reconcile decides; the Worker only schedules the
// next pass: "not finished" -> again after stepDelay, "failed" -> again after
// retryDelay, "done" -> nothing.
func RunWorker(
	ctx context.Context,
	queue *Queue,
	store WidgetStore,
	state InternalState,
	stepDelay, retryDelay time.Duration,
) {
	for ctx.Err() == nil {
		name, ok := queue.Take(100 * time.Millisecond) // Short wait, to notice ctx.
		if !ok {
			continue
		}
		needsAnotherPass, err := Reconcile(name, store, state)
		if err != nil {
			// The action failed; the next pass compares again from scratch.
			log.Printf("%s: reconcile failed, retrying in %v: %v", name, retryDelay, err)
			queue.AddAfter(name, retryDelay)
			continue
		}
		if widget, exists := store.Get(name); exists {
			result, _ := json.Marshal(widget.Result)
			log.Printf("%s: result %s", name, result)
		} else {
			log.Printf("%s: spec deleted, internal state removed", name)
		}
		if needsAnotherPass {
			queue.AddAfter(name, stepDelay)
		}
	}
}
