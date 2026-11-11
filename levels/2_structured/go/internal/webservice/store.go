// Package webservice is the web service variant: an HTTP API and an in-memory
// Store feed the shared core.
package webservice

import (
	"sync"

	"controlloop/internal/core"
)

// Store (web service) keeps the spec and latest result of each Widget, in memory.
//
// Kubernetes counterpart: etcd. Written by the HTTP API (spec) and by
// Reconcile (result). Writing or deleting a spec calls notify(name), which
// adds the name to the Queue. Writing a result does not: a result is a
// report, and checking again because of it would only find nothing to change.
//
// Fits the core's WidgetStore (Get, SetResult); PutSpec and Delete are used by
// the HTTP API. The HTTP API and the Worker run in different goroutines, so
// every method locks.
type Store struct {
	notify  func(name string) // Called with a name after its spec changes.
	mu      sync.Mutex
	widgets map[string]core.Widget
}

// NewStore returns an empty Store that calls notify after each spec change.
func NewStore(notify func(name string)) *Store {
	return &Store{notify: notify, widgets: map[string]core.Widget{}}
}

// PutSpec creates the Widget or replaces its spec. The previous result is
// kept until Reconcile writes a new one, so a result can be out of date.
func (s *Store) PutSpec(name string, spec core.Spec) {
	s.mu.Lock()
	widget := s.widgets[name]
	widget.Spec = spec
	s.widgets[name] = widget
	s.mu.Unlock()
	s.notify(name)
}

// Get returns a copy of the Widget; false if it does not exist.
// A copy, so changing it does not change the Store.
func (s *Store) Get(name string) (core.Widget, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	widget, exists := s.widgets[name]
	if exists && widget.Result != nil {
		result := *widget.Result // Copy the pointed-to result too.
		widget.Result = &result
	}
	return widget, exists
}

// Delete removes the Widget; false if it did not exist.
func (s *Store) Delete(name string) bool {
	s.mu.Lock()
	_, existed := s.widgets[name]
	delete(s.widgets, name)
	s.mu.Unlock()
	if existed {
		s.notify(name) // Reconcile then removes the internal state.
	}
	return existed
}

// SetResult writes the result; ignored if the Widget was deleted meanwhile.
func (s *Store) SetResult(name string, result core.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if widget, exists := s.widgets[name]; exists {
		widget.Result = &result
		s.widgets[name] = widget
	}
	return nil
}
