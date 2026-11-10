// Unit tests for the Store (web service): what it keeps and when it notifies.
//
// Tested: spec writes and deletes call notify (so the name reaches the
// Queue); result writes do not; a new spec keeps the old result; a result for a
// deleted name is dropped; Get returns copies.
//
// Not tested here: the lock under concurrent access (the smoke test runs the
// Store from several goroutines under `go test -race`, but does not try to
// provoke races), and the Queue itself: a slice stands in for it and records
// the notified names.
package webservice_test

import (
	"slices"
	"testing"

	"controlloop/internal/core"
	"controlloop/internal/webservice"
)

// newStore returns a Store and the slice of names it has passed to notify.
func newStore() (*webservice.Store, *[]string) {
	notified := &[]string{}
	return webservice.NewStore(func(name string) { *notified = append(*notified, name) }), notified
}

func TestWritingASpecNotifiesWithTheName(t *testing.T) {
	store, notified := newStore()
	store.PutSpec("demo", core.Spec{Replicas: 3})
	if !slices.Equal(*notified, []string{"demo"}) {
		t.Errorf("notified = %v", *notified)
	}
	widget, _ := store.Get("demo")
	if widget.Spec.Replicas != 3 || widget.Result != nil {
		t.Errorf("widget = %+v", widget)
	}
}

func TestWritingAResultDoesNotNotify(t *testing.T) {
	store, notified := newStore()
	store.PutSpec("demo", core.Spec{Replicas: 3})
	_ = store.SetResult("demo", core.Result{Replicas: 1})
	if !slices.Equal(*notified, []string{"demo"}) { // Only the spec write.
		t.Errorf("notified = %v", *notified)
	}
}

func TestNewSpecKeepsThePreviousResultUntilReconcileWritesOne(t *testing.T) {
	store, _ := newStore()
	store.PutSpec("demo", core.Spec{Replicas: 3})
	_ = store.SetResult("demo", core.Result{Replicas: 3, Ready: true})
	store.PutSpec("demo", core.Spec{Replicas: 5})
	widget, _ := store.Get("demo")
	if widget.Result == nil || *widget.Result != (core.Result{Replicas: 3, Ready: true}) {
		t.Errorf("result = %+v", widget.Result)
	}
}

func TestDeletingNotifiesAndRemoves(t *testing.T) {
	store, notified := newStore()
	store.PutSpec("demo", core.Spec{Replicas: 3})
	if !store.Delete("demo") {
		t.Error("Delete = false, want true")
	}
	if !slices.Equal(*notified, []string{"demo", "demo"}) {
		t.Errorf("notified = %v", *notified)
	}
	if _, exists := store.Get("demo"); exists {
		t.Error("demo still exists")
	}
}

func TestDeletingAnAbsentNameDoesNotNotify(t *testing.T) {
	store, notified := newStore()
	if store.Delete("demo") {
		t.Error("Delete = true, want false")
	}
	if len(*notified) != 0 {
		t.Errorf("notified = %v", *notified)
	}
}

func TestResultForADeletedNameIsIgnored(t *testing.T) {
	store, _ := newStore()
	_ = store.SetResult("demo", core.Result{Ready: true})
	if _, exists := store.Get("demo"); exists {
		t.Error("demo exists")
	}
}

func TestGetReturnsACopy(t *testing.T) {
	store, _ := newStore()
	store.PutSpec("demo", core.Spec{Replicas: 3})
	_ = store.SetResult("demo", core.Result{Replicas: 1})
	copy1, _ := store.Get("demo")
	copy1.Spec.Replicas = 99
	copy1.Result.Replicas = 99
	widget, _ := store.Get("demo")
	if widget.Spec.Replicas != 3 || widget.Result.Replicas != 1 {
		t.Errorf("widget = %+v, result %+v", widget, *widget.Result)
	}
}
