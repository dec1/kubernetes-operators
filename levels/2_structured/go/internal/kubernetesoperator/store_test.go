// Unit tests for the Store (operator): reading from the cache, writing status.
//
// Tested: Get turns a cached object into the core's Widget, and reports
// "absent" for a missing object or one being deleted; SetResult PATCHes the
// status, skips the PATCH when the status already holds the result, and
// ignores an object deleted meanwhile.
//
// Not tested here: how the cache is kept current (see watcher_test.go), and
// HTTP (the fake API stands in for the API server).
package kubernetesoperator_test

import (
	"context"
	"testing"

	"controlloop/internal/core"
	"controlloop/internal/kubernetesoperator"
)

// newStore returns a fake API holding Widget 'demo' with 3 replicas, and a
// Store whose cache holds the API's current objects, as after a list.
func newStore(t *testing.T) (*fakeAPI, *kubernetesoperator.Store) {
	t.Helper()
	api := newFakeAPI()
	api.Apply("demo", 3)
	store := kubernetesoperator.NewStore(api)
	objects, _, err := api.ListWidgets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	store.ReplaceAll(objects)
	return api, store
}

func TestGetReturnsSpecAndNoResultBeforeTheFirstStatus(t *testing.T) {
	_, store := newStore(t)
	widget, exists := store.Get("demo")
	if !exists || widget.Spec.Replicas != 3 || widget.Result != nil {
		t.Errorf("Get = %+v, %v", widget, exists)
	}
}

func TestGetReportsAnAbsentObject(t *testing.T) {
	_, store := newStore(t)
	if _, exists := store.Get("other"); exists {
		t.Error("other exists")
	}
}

func TestGetReportsAnObjectBeingDeletedAsAbsent(t *testing.T) {
	// The API server sets deletionTimestamp when deletion is requested but
	// cannot finish yet, eg while a finalizer is pending.
	api, store := newStore(t)
	object := api.Object("demo")
	timestamp := "2026-11-04T12:00:00Z"
	object.Metadata.DeletionTimestamp = &timestamp
	store.Put(object)
	if _, exists := store.Get("demo"); exists {
		t.Error("demo exists")
	}
}

func TestSetResultPatchesTheStatus(t *testing.T) {
	api, store := newStore(t)
	want := core.Result{Replicas: 1, Ready: false}
	if err := store.SetResult("demo", want); err != nil {
		t.Fatal(err)
	}
	if got := api.Status("demo"); got == nil || *got != want {
		t.Errorf("stored status = %+v", got)
	}
	if widget, _ := store.Get("demo"); widget.Result == nil || *widget.Result != want {
		t.Errorf("cached result = %+v", widget.Result)
	}
}

func TestSetResultSkipsThePatchWhenTheStatusIsUnchanged(t *testing.T) {
	api, store := newStore(t)
	_ = store.SetResult("demo", core.Result{Replicas: 1})
	_ = store.SetResult("demo", core.Result{Replicas: 1})
	if got := api.PatchCount(); got != 1 {
		t.Errorf("PATCHes = %d, want 1", got)
	}
}

func TestSetResultForAnObjectDeletedMeanwhileIsIgnored(t *testing.T) {
	api, store := newStore(t)
	api.Delete("demo") // Deleted in Kubernetes; the cache has not seen it yet.
	if err := store.SetResult("demo", core.Result{Replicas: 1}); err != nil {
		t.Errorf("err = %v, want none", err)
	}
}
