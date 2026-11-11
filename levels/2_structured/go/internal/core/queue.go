package core

import (
	"sync"
	"time"
)

// Queue holds names waiting for a check, first in, first out.
//
// A name only means "check this Widget"; the Worker reads the current spec
// when it takes the name. A name already waiting is not added again, so
// several spec writes before a check lead to one check. Safe for concurrent use.
type Queue struct {
	mu      sync.Mutex
	names   []string        // Waiting names, in order.
	waiting map[string]bool // Same names, for a fast duplicate check.
	signal  chan struct{}   // Wakes a Worker waiting in Take.
}

// NewQueue returns an empty Queue.
func NewQueue() *Queue {
	return &Queue{waiting: map[string]bool{}, signal: make(chan struct{}, 1)}
}

// Add adds the name unless it is already waiting.
func (q *Queue) Add(name string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.waiting[name] {
		return
	}
	q.waiting[name] = true
	q.names = append(q.names, name)
	select {
	case q.signal <- struct{}{}: // Wake a waiting Take.
	default: // A wake-up is already pending.
	}
}

// AddAfter adds the name after a delay, without blocking the caller.
// Used by the Worker for "check again shortly".
func (q *Queue) AddAfter(name string, delay time.Duration) {
	time.AfterFunc(delay, func() { q.Add(name) })
}

// Take removes and returns the oldest waiting name. It waits up to timeout;
// false if no name arrives.
func (q *Queue) Take(timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for {
		q.mu.Lock()
		if len(q.names) > 0 {
			name := q.names[0]
			q.names = q.names[1:]
			delete(q.waiting, name) // It may now be added again.
			q.mu.Unlock()
			return name, true
		}
		q.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", false
		}
		select {
		case <-q.signal:
		case <-time.After(remaining):
		}
	}
}
