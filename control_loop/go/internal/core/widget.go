// Package core is the part of the control loop that both variants share:
// Queue, Worker, Reconcile and InternalState, plus the Widget data.
//
// This file holds the one boundary between the core and a variant. The core
// only needs to read a Widget's spec and write its result; WidgetStore states
// exactly that. Each variant supplies its own WidgetStore:
//
//	web service           webservice.Store: specs and results in memory
//	Kubernetes operator   kubernetesoperator.Store: Widget objects in Kubernetes
//
// So the core never depends on a variant; a variant plugs into the core
// (inversion of control).
package core

// Spec is what the Client asks for.
type Spec struct {
	Replicas int `json:"replicas"`
}

// Result is what Reconcile last observed.
type Result struct {
	Replicas int  `json:"replicas"`
	Ready    bool `json:"ready"`
}

// Widget is a Widget as the core sees it: spec and latest result.
type Widget struct {
	Spec   Spec    `json:"spec"`
	Result *Result `json:"result"` // nil until Reconcile has written one.
}

// WidgetStore is what the core needs from a Store. Any type with these two
// methods fits; Go interfaces need no "implements" declaration.
type WidgetStore interface {
	// Get returns the Widget's current spec and result; false if it does not exist.
	Get(name string) (Widget, bool)
	// SetResult records the result; a Widget that no longer exists is ignored.
	SetResult(name string, result Result) error
}
