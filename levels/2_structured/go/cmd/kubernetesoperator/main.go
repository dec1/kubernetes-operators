// Command kubernetesoperator runs the control loop with Kubernetes storing the specs.
//
// Run against a cluster, from outside it:
//
//	kubectl proxy &
//	go run ./cmd/kubernetesoperator -namespace default
//
// Then apply a Widget object with spec.replicas: 3, and see the result with
// `kubectl get widget demo -o yaml`. This needs a cluster with the Widget CRD
// installed; the tests need neither.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"controlloop/internal/kubernetesoperator"
)

func main() {
	namespace := flag.String("namespace", "default", "namespace to watch")
	stepDelay := flag.Duration("step-delay", time.Second, "delay between passes while the spec is not met")
	retryDelay := flag.Duration("retry-delay", 2*time.Second, "delay before retrying a failed pass or watch")
	flag.Parse()

	api, err := kubernetesoperator.NewHTTPKubernetesAPIFromEnvironment(*namespace)
	if err != nil {
		log.Fatal(err)
	}
	operator := kubernetesoperator.New(api, *stepDelay, *retryDelay, 5*time.Minute)
	operator.Start()
	log.Printf("watching Widgets in namespace %q", *namespace)

	// Run until Ctrl+C or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	operator.Stop()
}
