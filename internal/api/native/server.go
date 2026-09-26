package native

import (
	"encoding/json"
	"net/http"

	"github.com/notborges/convomeow/internal/api/native/v1"
	"github.com/notborges/convomeow/internal/api/web"
	"github.com/notborges/convomeow/internal/app"
)

func New(service *app.Service, token string) http.Handler {
	return newServer(service, token, nil)
}

func NewWithWeb(service *app.Service, token, webDir string) (http.Handler, error) {
	ui, err := web.New(webDir, token)
	if err != nil {
		return nil, err
	}
	return newServer(service, token, ui), nil
}

func newServer(service *app.Service, token string, ui *web.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/versions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"versions": []string{"v1"}})
	})
	var browserAuth func(*http.Request) bool
	if ui != nil {
		browserAuth = ui.Authenticated
		mux.Handle("/app", ui)
		mux.Handle("/app/", ui)
	}
	mux.Handle("/api/v1/", v1.New(service, token, browserAuth))
	return mux
}
