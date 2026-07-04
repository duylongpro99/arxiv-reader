package server

import (
	"encoding/json"
	"net/http"
)

const version = "0.1.0"

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// healthHandler returns exactly {"status":"ok","version":"0.1.0"}.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	body, _ := json.Marshal(healthResponse{Status: "ok", Version: version})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
