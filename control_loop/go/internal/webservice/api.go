package webservice

import (
	"encoding/json"
	"net/http"
	"regexp"

	"controlloop/internal/core"
)

// NewHandler returns the HTTP API (web service): how a Client writes specs and
// reads results. Kubernetes counterpart: the API server. It only reads and
// writes the Store; it never calls Reconcile or changes the internal state.
//
//	PUT    /widgets/{name}   body {"spec": {"replicas": 3}}  -> 200, the Widget
//	GET    /widgets/{name}                                   -> 200, the Widget
//	DELETE /widgets/{name}                                   -> 204
//
// The returned Widget is {"name": ..., "spec": ..., "result": ...}; "result"
// is null until Reconcile has written one.
func NewHandler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /widgets/{name}", func(w http.ResponseWriter, r *http.Request) {
		name, ok := validName(w, r)
		if !ok {
			return
		}
		spec, ok := readSpec(r)
		if !ok {
			send(w, http.StatusBadRequest, errorBody(`body must be {"spec": {"replicas": <integer >= 0>}}`))
			return
		}
		store.PutSpec(name, spec)
		sendWidget(w, store, name)
	})
	mux.HandleFunc("GET /widgets/{name}", func(w http.ResponseWriter, r *http.Request) {
		if name, ok := validName(w, r); ok {
			sendWidget(w, store, name)
		}
	})
	mux.HandleFunc("DELETE /widgets/{name}", func(w http.ResponseWriter, r *http.Request) {
		name, ok := validName(w, r)
		if !ok {
			return
		}
		if store.Delete(name) {
			w.WriteHeader(http.StatusNoContent)
		} else {
			send(w, http.StatusNotFound, errorBody("widget not found"))
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { // Any other path.
		send(w, http.StatusNotFound, errorBody("use /widgets/{name}, name of a-z, 0-9 and -"))
	})
	return mux
}

var widgetName = regexp.MustCompile(`^[a-z0-9-]+$`)

// validName returns the name from /widgets/{name}; otherwise it sends 404.
func validName(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if !widgetName.MatchString(name) {
		send(w, http.StatusNotFound, errorBody("use /widgets/{name}, name of a-z, 0-9 and -"))
		return "", false
	}
	return name, true
}

// readSpec returns the spec from the body; false if invalid.
func readSpec(r *http.Request) (core.Spec, bool) {
	var body struct {
		Spec struct {
			Replicas *int `json:"replicas"` // A pointer, to tell "missing" from 0.
		} `json:"spec"`
	}
	// Decoding fails for non-integers such as 1.5, "3" or true.
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return core.Spec{}, false
	}
	if body.Spec.Replicas == nil || *body.Spec.Replicas < 0 {
		return core.Spec{}, false
	}
	return core.Spec{Replicas: *body.Spec.Replicas}, true
}

// sendWidget sends the Widget as stored now, or 404 if it does not exist.
func sendWidget(w http.ResponseWriter, store *Store, name string) {
	widget, exists := store.Get(name)
	if !exists {
		send(w, http.StatusNotFound, errorBody("widget not found"))
		return
	}
	send(w, http.StatusOK, struct {
		Name string `json:"name"`
		core.Widget
	}{name, widget})
}

func errorBody(message string) map[string]string { return map[string]string{"error": message} }

// send sends a status code and a JSON body.
func send(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
