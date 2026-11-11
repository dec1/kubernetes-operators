// Unit tests for the Watcher: from Kubernetes changes to cache and Queue.
//
// Tested: the first "LIST" fills the cache and queues every name; "WATCH"
// changes (added, modified, deleted) update the cache and queue the name; after
// 410 Gone the Watcher lists again and also queues names deleted while it
// could not watch; with bookmarks it follows the position and needs no new list.
//
// Not tested here: Reconcile and the Worker (no Worker runs; the tests read
// the Queue directly), and HTTP (the fake API stands in for the API server).
package kubernetesoperator_test

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"controlloop/internal/core"
	"controlloop/internal/kubernetesoperator"
	"controlloop/internal/testutil"
)

// parts is the fake API, and the cache and Queue the Watcher fills.
type parts struct {
	api   *fakeAPI
	store *kubernetesoperator.Store
	queue *core.Queue
}

func newParts() parts {
	api := newFakeAPI()
	return parts{api: api, store: kubernetesoperator.NewStore(api), queue: core.NewQueue()}
}

// startWatcher runs the Watcher in a goroutine (each watch lasts 50 ms); it
// is stopped when the test ends.
func (p parts) startWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		kubernetesoperator.NewWatcher(p.api, p.store, p.queue, 50*time.Millisecond, 10*time.Millisecond).Run(ctx)
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
}

// queuedNames takes every name waiting in the Queue right now, sorted.
func (p parts) queuedNames() []string {
	var names []string
	for {
		name, ok := p.queue.Take(0)
		if !ok {
			slices.Sort(names)
			return names
		}
		names = append(names, name)
	}
}

// cachedNames returns the cached names, sorted.
func (p parts) cachedNames() []string {
	return slices.Sorted(maps.Keys(p.store.Names()))
}

func TestFirstListFillsTheCacheAndQueuesEveryName(t *testing.T) {
	p := newParts()
	p.api.Apply("a", 1)
	p.api.Apply("b", 2)
	p.startWatcher(t)
	if !testutil.WaitUntil(func() bool { return slices.Equal(p.cachedNames(), []string{"a", "b"}) }) {
		t.Fatalf("cached = %v", p.cachedNames())
	}
	if got := p.queuedNames(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("queued = %v", got)
	}
}

func TestWatchedChangesUpdateTheCacheAndQueueTheName(t *testing.T) {
	p := newParts()
	p.startWatcher(t)
	if !testutil.WaitUntil(func() bool { return p.api.ListCount() == 1 }) {
		t.Fatal("no list")
	}

	p.api.Apply("demo", 3) // ADDED
	if !testutil.WaitUntil(func() bool { _, ok := p.store.Get("demo"); return ok }) {
		t.Fatal("ADDED not cached")
	}
	if got := p.queuedNames(); !slices.Equal(got, []string{"demo"}) {
		t.Errorf("after ADDED, queued = %v", got)
	}

	p.api.Apply("demo", 5) // MODIFIED
	if !testutil.WaitUntil(func() bool { w, _ := p.store.Get("demo"); return w.Spec.Replicas == 5 }) {
		t.Fatal("MODIFIED not cached")
	}
	if got := p.queuedNames(); !slices.Equal(got, []string{"demo"}) {
		t.Errorf("after MODIFIED, queued = %v", got)
	}

	p.api.Delete("demo") // DELETED
	if !testutil.WaitUntil(func() bool { _, ok := p.store.Get("demo"); return !ok }) {
		t.Fatal("DELETED still cached")
	}
	if got := p.queuedNames(); !slices.Equal(got, []string{"demo"}) {
		t.Errorf("after DELETED, queued = %v", got)
	}
}

func TestAfter410ItListsAgainAndQueuesNamesDeletedMeanwhile(t *testing.T) {
	p := newParts()
	p.api.Apply("kept", 1)
	p.api.Apply("gone", 1)
	p.startWatcher(t)
	if !testutil.WaitUntil(func() bool { return len(p.cachedNames()) == 2 }) {
		t.Fatal("first list not cached")
	}
	p.queuedNames() // Clear the names from the first list.

	// 'gone' is deleted while the Watcher cannot see it: the DELETED change is
	// never delivered, and the Watcher's position has expired.
	p.api.DeleteUnseen("gone")

	if !testutil.WaitUntil(func() bool { return p.api.ListCount() == 2 }) {
		t.Fatal("did not list again after 410")
	}
	if !testutil.WaitUntil(func() bool { return slices.Equal(p.cachedNames(), []string{"kept"}) }) {
		t.Fatalf("cached = %v", p.cachedNames())
	}
	// 'gone' is queued although no DELETED change arrived: Reconcile will find
	// no object and remove its internal state (the removed group).
	if got := p.queuedNames(); !slices.Contains(got, "gone") {
		t.Errorf("queued = %v, want gone among them", got)
	}
}

func TestWithBookmarksNoNewListIsNeeded(t *testing.T) {
	p := newParts()
	p.startWatcher(t)
	if !testutil.WaitUntil(func() bool { return p.api.ListCount() == 1 }) {
		t.Fatal("no list")
	}

	// Other cluster writes move the position; the Watcher learns the new
	// position from the bookmark and watches from it next. Then the history
	// before that position is dropped.
	p.api.SendBookmark()
	position := p.api.Position()
	if !testutil.WaitUntil(func() bool { return p.api.WatchedFrom() == position }) {
		t.Fatal("Watcher did not move to the bookmark's position")
	}
	p.api.ForgetHistory()

	p.api.Apply("demo", 1) // Arrives through the same, still valid, position.
	if !testutil.WaitUntil(func() bool { _, ok := p.store.Get("demo"); return ok }) {
		t.Fatal("change not received")
	}
	if got := p.api.ListCount(); got != 1 {
		t.Errorf("lists = %d, want 1 (no 410, so no second list)", got)
	}
}
