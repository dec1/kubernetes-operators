package kubernetesoperator

import (
	"context"
	"sync"
	"time"

	"controlloop/internal/core"
)

// KubernetesOperator creates and connects the parts; Start runs them in goroutines.
//
// Wiring: a spec written with kubectl is stored by the API server; the
// Watcher sees the change, updates the Store's cache and adds the name to the
// Queue; the Worker takes it and calls Reconcile, the same as in the web
// service. Compared with webservice.WebService, only the Store and the source
// of names differ.
//
// Takes any KubernetesAPI: the real HTTPKubernetesAPI in cmd/kubernetesoperator,
// the in-memory fake in the tests.
type KubernetesOperator struct {
	Queue         *core.Queue
	Store         *Store
	InternalState core.InternalState

	watcher    *Watcher
	stepDelay  time.Duration
	retryDelay time.Duration
	cancel     context.CancelFunc
	done       sync.WaitGroup
}

// New creates the parts; nothing runs until Start.
func New(api KubernetesAPI, stepDelay, retryDelay, watchTimeout time.Duration) *KubernetesOperator {
	queue := core.NewQueue()
	store := NewStore(api)
	return &KubernetesOperator{
		Queue:         queue,
		Store:         store,
		InternalState: core.NewInternalState(),
		watcher:       NewWatcher(api, store, queue, watchTimeout, retryDelay),
		stepDelay:     stepDelay,
		retryDelay:    retryDelay,
	}
}

// Start runs the Watcher and the Worker, each in its own goroutine. They share
// only the Store and the Queue, both of which lock.
func (o *KubernetesOperator) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	o.done.Add(2)
	go func() {
		defer o.done.Done()
		o.watcher.Run(ctx)
	}()
	go func() {
		defer o.done.Done()
		core.RunWorker(ctx, o.Queue, o.Store, o.InternalState, o.stepDelay, o.retryDelay)
	}()
}

// Stop stops both goroutines and waits until they have ended. Cancelling the
// context also ends an open watch at once.
func (o *KubernetesOperator) Stop() {
	o.cancel()
	o.done.Wait()
}
