package webservice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"controlloop/internal/core"
)

// WebService creates and connects the parts; Start runs them in goroutines.
//
// Wiring: a spec written through the HTTP API goes into the Store, whose
// notify adds the name to the Queue; the Worker takes it and calls Reconcile.
//
// Used by cmd/webservice and by the smoke test. Port 0 lets the system choose
// a free port; Port is the one actually used.
type WebService struct {
	Queue         *core.Queue
	Store         *Store
	InternalState core.InternalState
	Port          int

	listener   net.Listener
	server     *http.Server
	stepDelay  time.Duration
	retryDelay time.Duration
	cancel     context.CancelFunc
	done       sync.WaitGroup
}

// New creates the parts and opens the port; nothing runs until Start.
func New(port int, stepDelay, retryDelay time.Duration) (*WebService, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	queue := core.NewQueue()
	store := NewStore(queue.Add) // Spec changes add the name.
	return &WebService{
		Queue:         queue,
		Store:         store,
		InternalState: core.NewInternalState(),
		Port:          listener.Addr().(*net.TCPAddr).Port, // The actual port, also when 0 was given.
		listener:      listener,
		server:        &http.Server{Handler: NewHandler(store)},
		stepDelay:     stepDelay,
		retryDelay:    retryDelay,
	}, nil
}

// Start runs the HTTP server and the Worker, each in its own goroutine. The
// server handles each request in a goroutine of its own. Specs and results
// cross between them only through the Store, which locks.
func (s *WebService) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done.Add(2)
	go func() {
		defer s.done.Done()
		if err := s.server.Serve(s.listener); !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
	go func() {
		defer s.done.Done()
		core.RunWorker(ctx, s.Queue, s.Store, s.InternalState, s.stepDelay, s.retryDelay)
	}()
}

// Stop stops both goroutines and waits until they have ended.
func (s *WebService) Stop() {
	s.cancel()
	_ = s.server.Shutdown(context.Background()) // Ends Serve; also closes the listener.
	s.done.Wait()
}
