package kubernetesoperator

import (
	"context"
	"errors"
	"sync"

	"controlloop/internal/core"
)

// Store (operator) is the core's WidgetStore for the operator.
//
// Part of our operator, not of Kubernetes: it reads Widget objects that the
// API server keeps in etcd, and writes their status through the API server.
//
//	Get(name)         reads a local copy (cache) of the Widget object; no API call
//	SetResult(name)   writes the result into the object's status: PATCH to the API server
//
// The cache is kept current by the Watcher, through ReplaceAll, Put and
// Remove. Reading from it instead of calling the API server on every pass is
// what client libraries do too (their "informer cache").
//
// Fits the core's WidgetStore (Get, SetResult); the web service's Store is the
// same role, in memory. The Watcher and the Worker run in different
// goroutines, so every method locks.
type Store struct {
	api     KubernetesAPI
	mu      sync.Mutex
	objects map[string]Object // name -> latest Widget object seen
}

// NewStore returns a Store with an empty cache.
func NewStore(api KubernetesAPI) *Store {
	return &Store{api: api, objects: map[string]Object{}}
}

// ---- used by the core (WidgetStore)

// Get returns spec and result from the cached object; false if absent or
// being deleted.
func (s *Store) Get(name string) (core.Widget, bool) {
	s.mu.Lock()
	object, exists := s.objects[name]
	s.mu.Unlock()
	if !exists || object.Metadata.DeletionTimestamp != nil {
		return core.Widget{}, false
	}
	widget := core.Widget{Spec: object.Spec}
	if object.Status != nil {
		result := *object.Status // A copy, so changing it does not change the cache.
		widget.Result = &result
	}
	return widget, true
}

// SetResult PATCHes the object's status, unless it already holds this result.
func (s *Store) SetResult(name string, result core.Result) error {
	s.mu.Lock()
	object, exists := s.objects[name]
	s.mu.Unlock()
	if !exists || (object.Status != nil && *object.Status == result) {
		return nil // Deleted, or nothing new to report: no API call.
	}
	versionBefore := object.Metadata.ResourceVersion
	updated, err := s.api.PatchStatus(context.Background(), name, result)
	if errors.Is(err, ErrNotFound) {
		return nil // Deleted meanwhile; WidgetStore says: ignore.
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keep the PATCH response only if the Watcher has not stored a newer
	// object meanwhile. resourceVersions are opaque: compare for equality
	// only, never for "greater than".
	if current, exists := s.objects[name]; exists && current.Metadata.ResourceVersion == versionBefore {
		s.objects[name] = updated
	}
	return nil
}

// ---- used by the Watcher

// Names returns the names currently cached.
func (s *Store) Names() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := map[string]bool{}
	for name := range s.objects {
		names[name] = true
	}
	return names
}

// ReplaceAll makes the cache exactly these objects (after a list).
func (s *Store) ReplaceAll(objects []Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects = map[string]Object{}
	for _, object := range objects {
		s.objects[object.Metadata.Name] = object
	}
}

// Put stores one object (after a watched change).
func (s *Store) Put(object Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[object.Metadata.Name] = object
}

// Remove drops one object (after a watched deletion).
func (s *Store) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, name)
}
