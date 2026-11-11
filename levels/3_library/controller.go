// Library controller: the control loop on controller-runtime, the Go library
// that Kubernetes operators are usually built on (kubebuilder generates
// projects for it).
//
// The same Widget replicas example as Structured, with the same terms:
//   - spec: the client's request in a Widget object's spec, eg replicas: 3;
//   - internal state: the running replicas per name, a map in memory;
//   - result: what the controller reports, written to the object's status.
//
// Our code is only Reconcile and the wiring in run(). The library supplies the
// rest of Structured's operator: the API client, the cache kept current by
// "LIST" and "WATCH" (the Watcher and Store), the work queue and the worker
// (the Queue and Worker). It adds what Structured leaves out: retries with
// growing delays, several workers (MaxConcurrentReconciles, default 1), leader
// election and metrics.
//
// Code between "Begin/End : Widget Controller Specific" markers is specific to
// this controller; the rest is the same in every controller.
package main

// A name before an import path is an alias for that package in this file.
import (
	"context"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// --------------------- Begin : Widget Controller Specific ---------------------
// Widget objects are unstructured JSON maps here; kubebuilder projects
// instead generate typed Go structs.
var widgetGVK = schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}

// stepDelay is the pause before the next pass while the spec is not yet met.
const stepDelay = time.Second

// newWidget returns an empty Widget object for the library to fill.
func newWidget() *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(widgetGVK)
	return object
}

// --------------------- End   : Widget Controller Specific ---------------------

// Reconciler holds what Reconcile needs between calls.
type Reconciler struct {
	// Kubernetes API client: reads come from the library's cache, writes go
	// to the API server.
	kubernetesAPI client.Client
	// --------------------- Begin : Widget Controller Specific ---------------------
	// INTERNAL STATE: running replicas per name. No mutex: with one worker,
	// Reconcile never runs twice at once.
	internalState map[types.NamespacedName]int64
	// --------------------- End   : Widget Controller Specific ---------------------
}

// Reconcile is one pass of the control loop for one Widget; the library's
// worker calls it with a name taken from its queue. Its name and signature are
// fixed by the library. It receives a name, not an event, so creation, change
// and deletion all take this one path.
//
// The returned ctrl.Result says whether another pass is needed: RequeueAfter
// adds the name to the queue again after that delay; an empty Result means
// done. A returned error also adds the name again, with growing delays.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx) // Log lines include namespace and name.
	name := request.NamespacedName

	// Read the object from the library's cache, not from the API server.
	object := newWidget()
	err := r.kubernetesAPI.Get(ctx, name, object)

	// SPEC DELETED: object gone or marked for deletion.
	if apierrors.IsNotFound(err) || (err == nil && object.GetDeletionTimestamp() != nil) {
		// --------------------- Begin : Widget Controller Specific ---------------------
		// Remove the internal state (a map entry); no Kubernetes object is deleted.
		if _, exists := r.internalState[name]; exists {
			delete(r.internalState, name)
			logger.Info("spec deleted, internal state removed")
		}
		// --------------------- End   : Widget Controller Specific ---------------------
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// --------------------- Begin : Widget Controller Specific ---------------------
	// 1. SPEC: read what the client asked for.
	specifiedReplicas, _, err := unstructured.NestedInt64(object.Object, "spec", "replicas")
	if err != nil {
		return ctrl.Result{}, err
	}

	// 2. INTERNAL STATE: measure it.
	runningReplicas := r.internalState[name]

	// 3. Change the internal state by at most one replica per pass.
	switch {
	case runningReplicas < specifiedReplicas:
		runningReplicas++
	case runningReplicas > specifiedReplicas:
		runningReplicas--
	}
	r.internalState[name] = runningReplicas

	// 4. RESULT: what is running now.
	ready := runningReplicas == specifiedReplicas
	result := map[string]any{"replicas": runningReplicas, "ready": ready}
	// --------------------- End   : Widget Controller Specific ---------------------

	// Write status only if the result changed. equality.Semantic.DeepEqual
	// compares the two maps by value, field by field.
	currentStatus, _, _ := unstructured.NestedMap(object.Object, "status")
	if !equality.Semantic.DeepEqual(currentStatus, result) {
		// Merge patch: record the object before the change, so Patch sends
		// only the difference.
		statusPatch := client.MergeFrom(object.DeepCopy())
		if err := unstructured.SetNestedMap(object.Object, result, "status"); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.kubernetesAPI.Status().Patch(ctx, object, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("result", "replicas", runningReplicas, "ready", ready)
	}

	if !ready {
		return ctrl.Result{RequeueAfter: stepDelay}, nil // Another pass is needed.
	}
	return ctrl.Result{}, nil
}

// run configures the library for one namespace and starts it. It returns only
// when the process is stopped or setup fails.
func run(namespace string) error {
	// --------------------- Begin : Widget Controller Specific ---------------------
	managerOptions := ctrl.Options{
		// Cache only this namespace, so a namespaced Role suffices.
		Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
		// Reads of unstructured objects bypass the cache unless enabled here.
		Client: client.Options{Cache: &client.CacheOptions{Unstructured: true}},
		// No metrics listener, so the controller opens no port.
		Metrics: metricsserver.Options{BindAddress: "0"},
	}
	reconciler := &Reconciler{internalState: map[types.NamespacedName]int64{}}
	// --------------------- End   : Widget Controller Specific ---------------------

	// Load the API server address and credentials: in a Pod, its ServiceAccount
	// token and CA bundle; elsewhere, ~/.kube/config. "OrDie": exit the process
	// if neither is found.
	apiConfig := ctrl.GetConfigOrDie()

	// The manager owns the cache, the work queue and the Kubernetes API client.
	manager, err := ctrl.NewManager(apiConfig, managerOptions)
	if err != nil {
		return err
	}

	// Register the controller: watch Widgets (For) and pass each changed
	// object's name to the reconciler. GenerationChangedPredicate skips
	// changes that leave the spec unchanged: metadata.generation grows only
	// when the spec changes. Without it, our own status PATCH would queue the
	// name again at once, and stepDelay would have no effect.
	reconciler.kubernetesAPI = manager.GetClient()
	err = ctrl.NewControllerManagedBy(manager).
		For(newWidget(), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("widget").
		Complete(reconciler)
	if err != nil {
		return err
	}

	// Start runs until the process is stopped (SIGTERM or Ctrl+C):
	// 1. "LIST": GET all Widgets into the cache, and queue every name.
	// 2. "WATCH": GET with watch=true from the list's position (with
	//    bookmarks). Each change updates the cache and queues that name; a
	//    name already queued is not added again.
	// 3. After 410 Gone: "LIST" again. Names no longer listed produce delete
	//    notifications, so missed deletions reach Reconcile as NotFound.
	// Meanwhile the worker takes names from the queue and calls Reconcile.
	return manager.Start(ctrl.SetupSignalHandler())
}

func main() {
	// zap: a structured logging library; zap.New() builds a logger that writes
	// one JSON line per message. Without SetLogger, log lines are dropped.
	ctrl.SetLogger(zap.New())
	// --------------------- Begin : Widget Controller Specific ---------------------
	namespace := os.Getenv("WATCH_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	// --------------------- End   : Widget Controller Specific ---------------------
	if err := run(namespace); err != nil {
		ctrl.Log.Error(err, "controller stopped")
		os.Exit(1)
	}
}
