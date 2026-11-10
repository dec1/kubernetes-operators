// fakeAPI: an in-memory stand-in for the API server and its etcd, for tests.
//
// It behaves like the real server in the ways the operator depends on:
//
//   - objects and a change history, each change at a new position (resourceVersion);
//   - list returns all objects and the current position;
//   - watch replays the changes after a position, then waits for new ones
//     until its timeout; positions older than the kept history yield
//     ErrResourceExpired;
//   - a status PATCH changes the object (a new position) unless the status is
//     already equal, in which case nothing changes, as on the real server.
//
// The tests play the Client with Apply and Delete (what kubectl would do), and
// use ForgetHistory, DeleteUnseen and SendBookmark to trigger 410 Gone, a
// missed deletion and BOOKMARK.
//
// Not modelled: validation against a CRD schema, namespaces, deletionTimestamp
// and finalizers, authentication.
package kubernetesoperator_test

import (
	"context"
	"iter"
	"strconv"
	"sync"
	"time"

	"controlloop/internal/core"
	"controlloop/internal/kubernetesoperator"
)

type change struct {
	position int
	event    kubernetesoperator.WatchEvent
}

type fakeAPI struct {
	mu         sync.Mutex
	objects    map[string]kubernetesoperator.Object
	history    []change      // Oldest first.
	position   int           // Position of the latest change.
	oldestKept int           // Watches must start at or after this position.
	changed    chan struct{} // Closed and replaced on every change: wakes waiting watches.

	patchCount  int    // Status PATCHes received, including unchanged ones.
	listCount   int    // Lists served: shows whether a watcher had to list again.
	watchedFrom string // Position of the latest watch request.
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{objects: map[string]kubernetesoperator.Object{}, changed: make(chan struct{})}
}

// ---- the Client's side (what kubectl would do)

// Apply creates the Widget, or changes its spec.
func (f *fakeAPI) Apply(name string, replicas int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	object, exists := f.objects[name]
	object.Metadata.Name = name
	object.Spec = core.Spec{Replicas: replicas}
	if exists {
		f.record("MODIFIED", object)
	} else {
		f.record("ADDED", object)
	}
}

func (f *fakeAPI) Delete(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	object := f.objects[name]
	delete(f.objects, name)
	f.record("DELETED", object)
}

// DeleteUnseen deletes the object and drops the history in one step: watchers
// never receive the DELETED change, as if they were disconnected meanwhile.
func (f *fakeAPI) DeleteUnseen(name string) {
	f.Delete(name)
	f.ForgetHistory()
}

// ForgetHistory drops the change history, as the real server does after a
// while: watches from any earlier position now get 410 Gone.
func (f *fakeAPI) ForgetHistory() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history = nil
	f.oldestKept = f.position
}

// SendBookmark: other objects in the cluster changed, moving the position,
// but no Widget did. Watchers receive only a BOOKMARK with the new position;
// without it, their position would fall behind and later expire.
func (f *fakeAPI) SendBookmark() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.position++
	bookmark := kubernetesoperator.WatchEvent{Type: "BOOKMARK"}
	bookmark.Object.Metadata.ResourceVersion = strconv.Itoa(f.position)
	f.history = append(f.history, change{f.position, bookmark})
	f.wake()
}

// Position returns the current position, without counting as a list.
func (f *fakeAPI) Position() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strconv.Itoa(f.position)
}

// Status returns the stored object's status, as `kubectl get` would show it;
// nil if there is none or the object does not exist.
func (f *fakeAPI) Status(name string) *core.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyObject(f.objects[name]).Status
}

// Object returns a copy of the stored object.
func (f *fakeAPI) Object(name string) kubernetesoperator.Object {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyObject(f.objects[name])
}

func (f *fakeAPI) PatchCount() int     { f.mu.Lock(); defer f.mu.Unlock(); return f.patchCount }
func (f *fakeAPI) ListCount() int      { f.mu.Lock(); defer f.mu.Unlock(); return f.listCount }
func (f *fakeAPI) WatchedFrom() string { f.mu.Lock(); defer f.mu.Unlock(); return f.watchedFrom }

// ---- KubernetesAPI: the operator's side

func (f *fakeAPI) ListWidgets(context.Context) ([]kubernetesoperator.Object, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCount++
	objects := []kubernetesoperator.Object{}
	for _, object := range f.objects {
		objects = append(objects, copyObject(object))
	}
	return objects, strconv.Itoa(f.position), nil
}

func (f *fakeAPI) WatchWidgets(ctx context.Context, resourceVersion string, timeout time.Duration) iter.Seq2[kubernetesoperator.WatchEvent, error] {
	return func(yield func(kubernetesoperator.WatchEvent, error) bool) {
		f.mu.Lock()
		f.watchedFrom = resourceVersion
		position, _ := strconv.Atoi(resourceVersion)
		expired := position < f.oldestKept
		f.mu.Unlock()
		if expired {
			yield(kubernetesoperator.WatchEvent{}, kubernetesoperator.ErrResourceExpired)
			return
		}
		deadline := time.Now().Add(timeout)
		for {
			f.mu.Lock()
			var events []change
			for _, c := range f.history {
				if c.position > position {
					events = append(events, change{c.position, kubernetesoperator.WatchEvent{Type: c.event.Type, Object: copyObject(c.event.Object)}})
				}
			}
			changed := f.changed
			f.mu.Unlock()
			for _, c := range events { // Yield outside the lock.
				position = c.position
				if !yield(c.event, nil) {
					return
				}
			}
			if len(events) > 0 {
				continue
			}
			select { // Wait for the next change, the timeout, or cancellation.
			case <-changed:
			case <-time.After(time.Until(deadline)):
				return // Timeout: the server ends the response.
			case <-ctx.Done():
				return
			}
		}
	}
}

func (f *fakeAPI) PatchStatus(_ context.Context, name string, status core.Result) (kubernetesoperator.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patchCount++
	object, exists := f.objects[name]
	if !exists {
		return kubernetesoperator.Object{}, kubernetesoperator.ErrNotFound
	}
	if object.Status == nil || *object.Status != status {
		object.Status = &status
		f.record("MODIFIED", object)
	}
	return copyObject(f.objects[name]), nil
}

// ---- internal

// record gives the object a new position, stores it and appends the change to
// the history. Called with the lock held.
func (f *fakeAPI) record(changeType string, object kubernetesoperator.Object) {
	f.position++
	object.Metadata.ResourceVersion = strconv.Itoa(f.position)
	if changeType != "DELETED" {
		f.objects[object.Metadata.Name] = object
	}
	f.history = append(f.history, change{f.position, kubernetesoperator.WatchEvent{Type: changeType, Object: copyObject(object)}})
	f.wake()
}

// wake wakes every waiting watch. Called with the lock held.
func (f *fakeAPI) wake() {
	close(f.changed)
	f.changed = make(chan struct{})
}

// copyObject copies an object including what its pointers point to, so
// callers and the fake never share a status.
func copyObject(object kubernetesoperator.Object) kubernetesoperator.Object {
	if object.Status != nil {
		status := *object.Status
		object.Status = &status
	}
	if object.Metadata.DeletionTimestamp != nil {
		timestamp := *object.Metadata.DeletionTimestamp
		object.Metadata.DeletionTimestamp = &timestamp
	}
	return object
}
