// Command webservice runs the control loop with its own HTTP API and in-memory Store.
//
//	go run ./cmd/webservice -port 8080
//	curl -X PUT localhost:8080/widgets/demo -d '{"spec": {"replicas": 3}}'
//	curl localhost:8080/widgets/demo
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"controlloop/internal/webservice"
)

func main() {
	port := flag.Int("port", 8080, "port to listen on, on 127.0.0.1")
	stepDelay := flag.Duration("step-delay", time.Second, "delay between passes while the spec is not met")
	retryDelay := flag.Duration("retry-delay", 2*time.Second, "delay before retrying a failed pass")
	flag.Parse()

	service, err := webservice.New(*port, *stepDelay, *retryDelay)
	if err != nil {
		log.Fatal(err)
	}
	service.Start()
	log.Printf("listening on http://127.0.0.1:%d", service.Port)

	// Run until Ctrl+C or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	service.Stop()
}
