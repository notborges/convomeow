package v1

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
)

type requestIDKey struct{}

var errUnsupportedMediaType = errors.New("Content-Type must be application/json")

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

func authenticate(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, prefix) || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization, prefix)), []byte(token)) != 1 {
			writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Valid bearer token required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return errors.New("request must contain one JSON object")
		}
		return err
	}
	return nil
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := decodeJSON(w, r, dst); err != nil {
		var sizeError *http.MaxBytesError
		switch {
		case errors.Is(err, errUnsupportedMediaType):
			writeProblem(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error())
		case errors.As(err, &sizeError):
			writeProblem(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "JSON body exceeds 1 MiB.")
		default:
			writeProblem(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		}
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Type: "about:blank", Title: http.StatusText(status), Status: status,
		Detail: detail, Code: code, RequestID: requestID(r)})
}

func respondError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(err, core.ErrIdempotency):
		writeProblem(w, r, http.StatusConflict, "idempotency_conflict", "Idempotency-Key belongs to another request.")
	case errors.Is(err, core.ErrConflict):
		writeProblem(w, r, http.StatusConflict, "conflict", "Request conflicts with current state.")
	case errors.Is(err, core.ErrInvalid):
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, core.ErrNotConnected):
		writeProblem(w, r, http.StatusConflict, "account_not_connected", "Account is not connected.")
	case errors.Is(err, core.ErrMediaUnavailable):
		writeProblem(w, r, http.StatusGone, "media_unavailable", "Media is unavailable.")
	case errors.Is(err, core.ErrMediaTooLarge):
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "media_too_large", "Media exceeds the configured file size limit.")
	case errors.Is(err, core.ErrMediaQuota):
		writeProblem(w, r, http.StatusInsufficientStorage, "media_quota", "Media storage limit reached.")
	case errors.Is(err, core.ErrMediaBusy):
		writeProblem(w, r, http.StatusServiceUnavailable, "media_busy", "Media is busy. Retry shortly.")
	case errors.Is(err, core.ErrMediaStorage):
		writeProblem(w, r, http.StatusServiceUnavailable, "media_storage_unavailable", "Media storage is unavailable.")
	default:
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error.")
	}
}

func parsePage(w http.ResponseWriter, r *http.Request) (int, *core.PageCursor, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_query", "limit must be between 1 and 200.")
			return 0, nil, false
		}
		limit = parsed
	}
	cursor, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_query", "Invalid cursor.")
		return 0, nil, false
	}
	return limit, cursor, true
}

type cursorValue struct {
	At string `json:"at"`
	ID string `json:"id"`
}

func decodeCursor(raw string) (*core.PageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 512 {
		return nil, core.ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var value cursorValue
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(value.ID); err != nil {
		return nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, value.At)
	if err != nil {
		return nil, err
	}
	return &core.PageCursor{Time: at, ID: value.ID}, nil
}

func encodeCursor(at time.Time, id string) string {
	data, _ := json.Marshal(cursorValue{At: at.UTC().Format(time.RFC3339Nano), ID: id})
	return base64.RawURLEncoding.EncodeToString(data)
}
