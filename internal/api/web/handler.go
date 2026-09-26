package web

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const sessionCookie = "convomeow_session"

type Handler struct {
	files fs.FS
	token string
}

func New(dir, token string) (*Handler, error) {
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return nil, err
	}
	return &Handler{files: os.DirFS(dir), token: token}, nil
}

func (h *Handler) Authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || !h.validSession(cookie.Value, time.Now()) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		return sameOrigin(r)
	}
	return true
}

func sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && (origin.Scheme == "http" || origin.Scheme == "https") && origin.Host == r.Host
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/app":
		http.Redirect(w, r, "/app/", http.StatusPermanentRedirect)
	case "/app/session":
		h.sessionRequest(w, r)
	default:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.file(w, r)
	}
}

func (h *Handler) sessionRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"authenticated": h.Authenticated(r)})
	case http.MethodPost:
		if !sameOrigin(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			subtle.ConstantTimeCompare([]byte(body.Token), []byte(h.token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.setCookie(w, r, h.newSession(time.Now()), int(sessionLifetime.Seconds()))
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if !h.Authenticated(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.setCookie(w, r, "", -1)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", MaxAge: maxAge})
}

func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/app/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/app/")
	if name == "" {
		name = "index.html"
	}
	info, err := fs.Stat(h.files, name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) || strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	} else if info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, h.files, name)
}
