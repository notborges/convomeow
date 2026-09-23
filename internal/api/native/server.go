package native

import (
	"encoding/json"
	"net/http"

	"github.com/notborges/convomeow/internal/api/native/v1"
	"github.com/notborges/convomeow/internal/app"
)

func New(service *app.Service, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/versions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"versions": []string{"v1"}})
	})
	mux.Handle("/api/v1/", v1.New(service, token))
	return mux
}
