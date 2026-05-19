package api

import (
	"encoding/json"
	"net/http"
)

// healthHandler — liveness/readiness проба сервиса.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "rubezh-api",
	})
}
