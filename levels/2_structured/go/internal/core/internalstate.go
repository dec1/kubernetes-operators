package core

import "sync"

// InternalState is the running replicas of each Widget: what Reconcile changes.
//
// It stands in for the real thing a control loop manages, eg processes or
// cloud machines. An interface, so tests can supply one whose actions fail.
type InternalState interface {
	// Replicas returns the running replicas for the name; 0 if it has none.
	Replicas(name string) int
	// AddReplica starts one more replica.
	AddReplica(name string) error
	// RemoveReplica stops one replica.
	RemoveReplica(name string) error
	// Remove removes everything for the name; no effect if it has nothing.
	Remove(name string)
}

// memoryState is the InternalState used by both variants: a counter per name
// in memory. Only the Worker changes it, but tests read it meanwhile, so it
// locks (Go's race detector would report the unlocked map otherwise).
type memoryState struct {
	mu       sync.Mutex
	replicas map[string]int // name -> running replicas
}

// NewInternalState returns an empty in-memory InternalState.
func NewInternalState() InternalState {
	return &memoryState{replicas: map[string]int{}}
}

func (s *memoryState) Replicas(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replicas[name]
}

// AddReplica would start a process in a real system; here it counts up.
func (s *memoryState) AddReplica(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replicas[name]++
	return nil
}

// RemoveReplica would stop a process in a real system; here it counts down.
func (s *memoryState) RemoveReplica(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replicas[name]--
	return nil
}

func (s *memoryState) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.replicas, name)
}
