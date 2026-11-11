// Package kubernetesoperator is the Kubernetes operator variant: Kubernetes
// stores the specs, and a Watcher feeds the shared core.
package kubernetesoperator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"controlloop/internal/core"
)

// The API client makes the three calls the operator needs from the API server:
//
//	list     GET   .../widgets                         all Widget objects + position
//	watch    GET   .../widgets?watch=true&...          changes after a position
//	status   PATCH .../widgets/{name}/status           write the result
//
// KubernetesAPI states these calls; HTTPKubernetesAPI makes them over HTTP, and
// the tests use an in-memory fake with the same methods (fakeapi_test.go).
//
// A position is the resourceVersion: an opaque string marking a point in the
// API server's change history. The server keeps that history only briefly; a
// watch from a position it no longer has fails with 410 Gone
// (ErrResourceExpired), and only a fresh list recovers.

// Object is a Widget object as Kubernetes stores it. Only the fields the
// operator uses are declared; JSON decoding ignores the rest.
type Object struct {
	Metadata Metadata     `json:"metadata"`
	Spec     core.Spec    `json:"spec"`
	Status   *core.Result `json:"status,omitempty"` // nil until the operator writes one.
}

// Metadata is the part of an object's metadata the operator uses.
type Metadata struct {
	Name              string  `json:"name"`
	ResourceVersion   string  `json:"resourceVersion"`
	DeletionTimestamp *string `json:"deletionTimestamp,omitempty"` // Set when deletion was requested.
}

// WatchEvent is one change: Type is ADDED, MODIFIED, DELETED or BOOKMARK.
type WatchEvent struct {
	Type   string `json:"type"`
	Object Object `json:"object"`
}

var (
	// ErrResourceExpired is 410 Gone: the changes after this position are no
	// longer available.
	ErrResourceExpired = errors.New("resource version expired (410 Gone)")
	// ErrNotFound is 404: the object does not exist (any more).
	ErrNotFound = errors.New("not found (404)")
)

// KubernetesAPI is what the operator needs from the API server, for one namespace.
type KubernetesAPI interface {
	// ListWidgets returns all Widget objects and the position of this list.
	ListWidgets(ctx context.Context) ([]Object, string, error)
	// WatchWidgets yields each change after resourceVersion as it happens, for
	// up to timeout. It yields ErrResourceExpired if that position is too old.
	WatchWidgets(ctx context.Context, resourceVersion string, timeout time.Duration) iter.Seq2[WatchEvent, error]
	// PatchStatus writes the object's status and returns the updated object.
	// It returns ErrNotFound if the object does not exist.
	PatchStatus(ctx context.Context, name string, status core.Result) (Object, error)
}

// HTTPKubernetesAPI is KubernetesAPI over HTTP, standard library only.
//
// Inside a Pod: uses the Pod's ServiceAccount token and CA bundle.
// Elsewhere: talks plain HTTP to `kubectl proxy` (default 127.0.0.1:8001),
// which adds your kubeconfig credentials. No kubeconfig parsing needed here.
//
// Not covered by the tests: they use the fake instead.
type HTTPKubernetesAPI struct {
	widgetsURL string
	inCluster  bool
	client     *http.Client
}

const serviceAccount = "/var/run/secrets/kubernetes.io/serviceaccount"

// NewHTTPKubernetesAPIFromEnvironment chooses in-cluster or kubectl proxy:
// in a Pod, Kubernetes sets KUBERNETES_SERVICE_HOST.
func NewHTTPKubernetesAPIFromEnvironment(namespace string) (*HTTPKubernetesAPI, error) {
	api := &HTTPKubernetesAPI{client: &http.Client{}}
	base := os.Getenv("KUBERNETES_API_URL")
	if base == "" {
		base = "http://127.0.0.1:8001"
	}
	if host := os.Getenv("KUBERNETES_SERVICE_HOST"); host != "" {
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if port == "" {
			port = "443"
		}
		ca, err := os.ReadFile(serviceAccount + "/ca.crt")
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		api.client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
		api.inCluster = true
		base = "https://" + host + ":" + port
	}
	api.widgetsURL = fmt.Sprintf("%s/apis/example.com/v1/namespaces/%s/widgets", base, namespace)
	return api, nil
}

func (a *HTTPKubernetesAPI) ListWidgets(ctx context.Context) ([]Object, string, error) {
	var list struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Items []Object `json:"items"`
	}
	resp, err := a.do(ctx, http.MethodGet, a.widgetsURL, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, "", err
	}
	return list.Items, list.Metadata.ResourceVersion, nil
}

func (a *HTTPKubernetesAPI) WatchWidgets(ctx context.Context, resourceVersion string, timeout time.Duration) iter.Seq2[WatchEvent, error] {
	return func(yield func(WatchEvent, error) bool) {
		query := url.Values{
			"watch":               {"true"},
			"resourceVersion":     {resourceVersion},                    // Send changes after this position.
			"timeoutSeconds":      {fmt.Sprint(int(timeout.Seconds()))}, // The server ends the response then.
			"allowWatchBookmarks": {"true"},                             // Fresh positions even when nothing changes.
		}
		resp, err := a.do(ctx, http.MethodGet, a.widgetsURL+"?"+query.Encode(), nil)
		if err != nil {
			yield(WatchEvent{}, err)
			return
		}
		defer resp.Body.Close()
		lines := bufio.NewScanner(resp.Body) // One JSON event per line; waits for the next.
		lines.Buffer(make([]byte, 1<<20), 1<<20)
		for lines.Scan() {
			if len(bytes.TrimSpace(lines.Bytes())) == 0 {
				continue
			}
			var raw struct {
				Type   string          `json:"type"`
				Object json.RawMessage `json:"object"`
			}
			if err := json.Unmarshal(lines.Bytes(), &raw); err != nil {
				yield(WatchEvent{}, err)
				return
			}
			if raw.Type == "ERROR" { // The object is then a Status, not a Widget.
				var status struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}
				_ = json.Unmarshal(raw.Object, &status)
				if status.Code == http.StatusGone {
					yield(WatchEvent{}, ErrResourceExpired)
				} else {
					yield(WatchEvent{}, fmt.Errorf("watch error %d: %s", status.Code, status.Message))
				}
				return
			}
			event := WatchEvent{Type: raw.Type}
			if err := json.Unmarshal(raw.Object, &event.Object); err != nil {
				yield(WatchEvent{}, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
		if err := lines.Err(); err != nil {
			yield(WatchEvent{}, err)
		}
	}
}

func (a *HTTPKubernetesAPI) PatchStatus(ctx context.Context, name string, status core.Result) (Object, error) {
	body, _ := json.Marshal(map[string]core.Result{"status": status})
	resp, err := a.do(ctx, http.MethodPatch, a.widgetsURL+"/"+name+"/status", body)
	if err != nil {
		return Object{}, err
	}
	defer resp.Body.Close()
	var object Object
	err = json.NewDecoder(resp.Body).Decode(&object)
	return object, err
}

// do sends one request and turns 404 and 410 into ErrNotFound and ErrResourceExpired.
func (a *HTTPKubernetesAPI) do(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/merge-patch+json")
	}
	if a.inCluster {
		// Read on every call: Kubernetes rotates the token file.
		token, err := os.ReadFile(serviceAccount + "/token")
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusGone:
		resp.Body.Close()
		return nil, ErrResourceExpired
	case resp.StatusCode >= 300:
		resp.Body.Close()
		return nil, fmt.Errorf("%s %s: %s", method, url, resp.Status)
	}
	return resp, nil
}
