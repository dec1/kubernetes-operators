// Smoke test: run the whole web service and use it over HTTP, as a Client would.
//
// Tested: the parts are wired together correctly. A PUT is answered at once,
// the result then reaches the spec one replica at a time, a lowered spec is
// reached too, DELETE removes the Widget and its internal state, and an
// invalid spec is rejected.
//
// Not tested here: each decision in detail (see the core tests), the Worker's
// retry after a failed pass (see core/worker_test.go), and behaviour under load.
//
// Uses a free port (0) and a short step delay, and waits with
// testutil.WaitUntil instead of fixed sleeps.
package webservice_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"controlloop/internal/core"
	"controlloop/internal/testutil"
	"controlloop/internal/webservice"
)

// start returns a running web service; it is stopped when the test ends.
func start(t *testing.T) *webservice.WebService {
	t.Helper()
	service, err := webservice.New(0, 10*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	service.Start()
	t.Cleanup(service.Stop)
	return service
}

// response is the JSON body of any reply: a Widget, or an error.
type response struct {
	Name   string       `json:"name"`
	Result *core.Result `json:"result"`
	Error  string       `json:"error"`
}

// request sends one HTTP request; it returns the status and the decoded body.
func request(t *testing.T, service *webservice.WebService, method, name, body string) (int, response) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/widgets/%s", service.Port, name)
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded response
	_ = json.NewDecoder(resp.Body).Decode(&decoded) // Nothing to decode for 204.
	return resp.StatusCode, decoded
}

// resultIs reports whether GET /widgets/{name} currently returns this result.
func resultIs(t *testing.T, service *webservice.WebService, name string, want core.Result) bool {
	_, widget := request(t, service, "GET", name, "")
	return widget.Result != nil && *widget.Result == want
}

func TestSpecChangesAreReachedAndDeletionCleansUp(t *testing.T) {
	service := start(t)

	status, widget := request(t, service, "PUT", "demo", `{"spec": {"replicas": 3}}`)
	if status != http.StatusOK || widget.Name != "demo" || widget.Result != nil {
		t.Fatalf("PUT: status %d, widget %v", status, widget)
	}
	if !testutil.WaitUntil(func() bool { return resultIs(t, service, "demo", core.Result{Replicas: 3, Ready: true}) }) {
		t.Fatal("never reached 3 replicas")
	}

	request(t, service, "PUT", "demo", `{"spec": {"replicas": 1}}`)
	if !testutil.WaitUntil(func() bool { return resultIs(t, service, "demo", core.Result{Replicas: 1, Ready: true}) }) {
		t.Fatal("never reached 1 replica")
	}

	if status, _ := request(t, service, "DELETE", "demo", ""); status != http.StatusNoContent {
		t.Errorf("DELETE: status %d", status)
	}
	if status, _ := request(t, service, "GET", "demo", ""); status != http.StatusNotFound {
		t.Errorf("GET after DELETE: status %d", status)
	}
	if !testutil.WaitUntil(func() bool { return service.InternalState.Replicas("demo") == 0 }) {
		t.Error("internal state not removed")
	}
}

func TestInvalidSpecIsRejected(t *testing.T) {
	service := start(t)
	for _, body := range []string{`{"spec": {"replicas": -1}}`, `{"spec": {"replicas": 1.5}}`, `{"spec": {}}`, `not json`} {
		status, reply := request(t, service, "PUT", "demo", body)
		if status != http.StatusBadRequest || reply.Error == "" {
			t.Errorf("%s: status %d, body %+v", body, status, reply)
		}
	}
}
