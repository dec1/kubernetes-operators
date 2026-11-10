package kubernetesoperator

import (
	"context"
	"errors"
	"log"
	"time"

	"controlloop/internal/core"
)

// Watcher (operator) turns Kubernetes changes into names for the Queue.
//
// It replaces the web service's Store notify. Each change updates the Store's
// cache and adds the object's name to the Queue:
//
//  1. "LIST": all Widget objects and the current position (resourceVersion).
//     The cache becomes exactly these objects. Every name listed, plus every
//     name cached before but no longer listed (deleted while not watching),
//     goes to the Queue: the added, remained and removed groups.
//  2. "WATCH" from that position: each ADDED, MODIFIED or DELETED change
//     updates the cache and queues the name; BOOKMARK only moves the position.
//     When the server ends the response (timeout), watch again from the
//     latest position: no new list needed.
//  3. 410 Gone (ErrResourceExpired): the position is too old; back to 1.
//     Any other error: wait, then watch again from the same position.
type Watcher struct {
	api          KubernetesAPI
	store        *Store
	queue        *core.Queue
	watchTimeout time.Duration
	retryDelay   time.Duration
}

// NewWatcher returns a Watcher; Run starts it.
func NewWatcher(api KubernetesAPI, store *Store, queue *core.Queue, watchTimeout, retryDelay time.Duration) *Watcher {
	return &Watcher{api: api, store: store, queue: queue, watchTimeout: watchTimeout, retryDelay: retryDelay}
}

// Run runs until ctx is cancelled; meant for its own goroutine.
func (w *Watcher) Run(ctx context.Context) {
	resourceVersion := "" // Empty: a list is needed first.
	for ctx.Err() == nil {
		var err error
		if resourceVersion == "" {
			resourceVersion, err = w.list(ctx)
		}
		if err == nil {
			resourceVersion, err = w.watch(ctx, resourceVersion)
		}
		switch {
		case err == nil || ctx.Err() != nil:
		case errors.Is(err, ErrResourceExpired):
			log.Print("watch position expired (410 Gone): listing again")
			resourceVersion = ""
		default:
			log.Printf("watch failed, retrying in %v: %v", w.retryDelay, err)
			select {
			case <-ctx.Done():
			case <-time.After(w.retryDelay):
			}
		}
	}
}

// list is step 1: refill the cache and queue every name that may have changed.
func (w *Watcher) list(ctx context.Context) (string, error) {
	objects, resourceVersion, err := w.api.ListWidgets(ctx)
	if err != nil {
		return "", err
	}
	names := w.store.Names() // Cached before the list.
	w.store.ReplaceAll(objects)
	for name := range w.store.Names() {
		names[name] = true
	}
	for name := range names {
		w.queue.Add(name)
	}
	return resourceVersion, nil
}

// watch is step 2: apply each change; return the latest position.
func (w *Watcher) watch(ctx context.Context, resourceVersion string) (string, error) {
	for event, err := range w.api.WatchWidgets(ctx, resourceVersion, w.watchTimeout) {
		if err != nil {
			return resourceVersion, err
		}
		resourceVersion = event.Object.Metadata.ResourceVersion
		if event.Type == "BOOKMARK" {
			continue // No object changed; only the position moved.
		}
		name := event.Object.Metadata.Name
		if event.Type == "DELETED" {
			w.store.Remove(name)
		} else {
			w.store.Put(event.Object)
		}
		w.queue.Add(name)
	}
	return resourceVersion, nil
}
