// Smoke test: run the whole operator against the fake API, as kubectl would use it.
//
// Tested: the parts are wired together correctly. A Widget applied to the API
// server reaches its spec one replica at a time, with the result written to
// the object's status; a lowered spec is reached too; deleting the object
// removes its internal state; after 410 Gone the operator recovers on its own.
//
// Not tested here: each part in detail (see the other tests), the real HTTP
// client (needs a cluster), and behaviour under load.
package kubernetesoperator_test

import (
	"testing"
	"time"

	"controlloop/internal/core"
	"controlloop/internal/kubernetesoperator"
	"controlloop/internal/testutil"
)

// startOperator returns a fake API and a running operator using it; the
// operator is stopped when the test ends.
func startOperator(t *testing.T) (*fakeAPI, *kubernetesoperator.KubernetesOperator) {
	t.Helper()
	api := newFakeAPI()
	operator := kubernetesoperator.New(api, 10*time.Millisecond, 10*time.Millisecond, 50*time.Millisecond)
	operator.Start()
	t.Cleanup(operator.Stop)
	return api, operator
}

// statusIs reports whether the object's status, as `kubectl get` would show
// it, equals want.
func statusIs(api *fakeAPI, name string, want core.Result) bool {
	status := api.Status(name)
	return status != nil && *status == want
}

func TestOperatorReachesSpecChangesAndCleansUpDeletion(t *testing.T) {
	api, operator := startOperator(t)

	api.Apply("demo", 3)
	if !testutil.WaitUntil(func() bool { return statusIs(api, "demo", core.Result{Replicas: 3, Ready: true}) }) {
		t.Fatalf("status = %+v", api.Status("demo"))
	}

	api.Apply("demo", 1)
	if !testutil.WaitUntil(func() bool { return statusIs(api, "demo", core.Result{Replicas: 1, Ready: true}) }) {
		t.Fatalf("status = %+v", api.Status("demo"))
	}

	api.Delete("demo")
	if !testutil.WaitUntil(func() bool { return operator.InternalState.Replicas("demo") == 0 }) {
		t.Error("internal state not removed")
	}
}

func TestOperatorRecoversAfter410AndCleansUpAMissedDeletion(t *testing.T) {
	api, operator := startOperator(t)

	api.Apply("demo", 2)
	if !testutil.WaitUntil(func() bool { return statusIs(api, "demo", core.Result{Replicas: 2, Ready: true}) }) {
		t.Fatalf("status = %+v", api.Status("demo"))
	}

	api.DeleteUnseen("demo") // The DELETED change is never delivered.
	if !testutil.WaitUntil(func() bool { return operator.InternalState.Replicas("demo") == 0 }) {
		t.Fatal("internal state not removed")
	}

	api.Apply("next", 1) // Watching works again after the new list.
	if !testutil.WaitUntil(func() bool { return statusIs(api, "next", core.Result{Replicas: 1, Ready: true}) }) {
		t.Errorf("status = %+v", api.Status("next"))
	}
}
